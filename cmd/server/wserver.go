package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

type watcherMetrics struct {
	clientsOnline *prometheus.GaugeVec
	imagesPut     prometheus.Counter
	imagesGet     prometheus.Counter
	lastUpload    prometheus.Gauge
}

type WatcherServer struct {
	client      mqtt.Client
	lastimg     *StoredImage
	clients     map[string]ClientStatus
	clientMutex *sync.RWMutex
	imgMutex    *sync.RWMutex

	metrics watcherMetrics

	Dir         string
	ListenAddr  string
	ExternalURL string
	Broker      string
}

func (ws *WatcherServer) setupMetrics() {
	ws.metrics.clientsOnline = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "phatmqtt",
		Subsystem: "server",
		Name:      "online_clients",
	}, []string{
		"status",
	})
	ws.metrics.imagesPut = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "phatmqtt",
		Subsystem: "server",
		Name:      "images_posted",
	})
	ws.metrics.imagesGet = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "phatmqtt",
		Subsystem: "server",
		Name:      "images_downloaded",
	})
	ws.metrics.lastUpload = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "phatmqtt",
		Subsystem: "server",
		Name:      "last_upload_at",
	})
}

func (ws *WatcherServer) clientUpdate(cl mqtt.Client, msg mqtt.Message) {
	topicparts := strings.SplitN(msg.Topic(), "/", 3)
	log.WithFields(log.Fields{
		"client": topicparts[2],
		"status": string(msg.Payload()),
	}).Info("client status update")
	ws.clientMutex.Lock()
	defer ws.clientMutex.Unlock()
	ws.clients[topicparts[2]] = ClientStatus{
		Status:   ClientStatusStatus(msg.Payload()),
		LastSeen: time.Now(),
	}
	status := make(map[string]int)
	for _, c := range ws.clients {
		status[string(c.Status)]++
	}
	for st := range status {
		ws.metrics.clientsOnline.WithLabelValues(st).Set(float64(status[st]))
	}
}

func (ws *WatcherServer) mqttOpts() *mqtt.ClientOptions {
	opts := mqtt.NewClientOptions()
	opts.SetClientID("phatmqttserver")
	opts.AddBroker(ws.Broker)
	opts.SetWill("phatserver/status", "DEAD", byte(1), true)
	opts.OnConnect = func(client mqtt.Client) {
		client.Publish("phatserver/status", byte(1), true, "ALIVE")
		log.WithField("broker", ws.Broker).Info("MQTT broker connected")
		client.Subscribe("phat/client/+", byte(1), ws.clientUpdate).Wait()
	}
	// opts.SetAutoReconnect(true)
	return opts
}

func (ws *WatcherServer) publishPost() error {
	ws.imgMutex.RLock()
	defer ws.imgMutex.RUnlock()
	if ws.lastimg != nil {
		u := strings.Join([]string{ws.ExternalURL, "image"}, "/")
		imgd, err := ws.lastimg.JSONRepresentation(u)
		if err != nil {
			return err
		}
		ws.client.Publish("phat/image", byte(1), false, imgd).Wait()
		// log.Info("published")
		return nil
	} else {
		return errors.New("not ready")
	}
}

func (ws *WatcherServer) ImageDownload(rw http.ResponseWriter, req *http.Request) {
	ws.imgMutex.RLock()
	defer ws.imgMutex.RUnlock()
	if ws.lastimg == nil {
		log.Warn("GET request but no image ready for download")
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(rw, "No image cached yet\n")
		return
	} else {
		log.WithField("format", ws.lastimg.Type).Info("GET request, serving cached image")
		ws.metrics.imagesGet.Inc()
		rw.Header().Add("Content-Type", ws.lastimg.Type)
		rw.Header().Set("ETag", ws.lastimg.ETag())
		// wb, err := rw.Write(ws.lastimg.image)
		http.ServeContent(rw, req, "", ws.lastimg.StoredAt, bytes.NewReader(ws.lastimg.Image))
	}
}

func (ws *WatcherServer) ListClients(rw http.ResponseWriter, req *http.Request) {
	type clientsListOutput struct {
		Clients map[string]ClientStatus `json:"clients"`
	}
	ws.clientMutex.RLock()
	defer ws.clientMutex.RUnlock()
	output := clientsListOutput{
		Clients: ws.clients,
	}
	rw.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(rw)
	err := enc.Encode(output)
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(rw, "unable to encode JSON: %s", err)
	}
}

func (ws *WatcherServer) ImageUpload(rw http.ResponseWriter, req *http.Request) {
	var err error
	if strings.HasPrefix(req.Header.Get("Content-Type"), "image/") {
		ws.imgMutex.Lock()
		defer ws.imgMutex.Unlock()
		imageIsRawVal := req.URL.Query().Get("raw")
		ws.lastimg, err = CreateImage(req.Body, imageIsRawVal != "true")
		if err != nil {
			rw.WriteHeader(http.StatusNotAcceptable)
			fmt.Fprintf(rw, "ERROR: %s", err.Error())
			return
		}
		rw.WriteHeader(http.StatusCreated)
		log.WithFields(log.Fields{
			"size":   len(ws.lastimg.Image),
			"format": ws.lastimg.Type,
		}).Info("stored image")
		ws.metrics.imagesPut.Inc()
		ws.metrics.lastUpload.SetToCurrentTime()
		// Actually push the thing to MQTT
		go ws.publishPost()
		return
	} else {
		rw.WriteHeader(http.StatusNotAcceptable)
		fmt.Fprintf(rw, "ERROR: Content-type not image/*")
		return
	}
}

func (ws *WatcherServer) runPeriodicPublisher(every time.Duration) {
	ticker := time.Tick(every)
	for {
		<-ticker
		ws.publishPost()
	}
}

func (ws *WatcherServer) runSystemdWatchdog(wdt time.Duration) {
	ticker := time.Tick(wdt / 2)
	for {
		<-ticker
		if ws.client.IsConnected() {
			log.Trace("pinging systemd watchdog")
			daemon.SdNotify(false, daemon.SdNotifyWatchdog)
		} else {
			log.Warning("skipping sd notify due to disconnected MQTT client")
		}
	}
}

func (ws *WatcherServer) Run(ctx context.Context) error {
	ws.clientMutex = &sync.RWMutex{}
	ws.imgMutex = &sync.RWMutex{}
	ws.clients = make(map[string]ClientStatus)
	ws.client = mqtt.NewClient(ws.mqttOpts())
	if token := ws.client.Connect(); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	ws.setupMetrics()
	go ws.runPeriodicPublisher(10 * 60 * time.Second)
	r := mux.NewRouter()
	r.Use(logMw)
	r.Methods(http.MethodPut).HandlerFunc(ws.ImageUpload)
	getroutes := r.Methods(http.MethodGet).Subrouter()
	getroutes.Path("/metrics").Handler(promhttp.Handler())
	getroutes.Path("/image").HandlerFunc(ws.ImageDownload)
	getroutes.Path("/").HandlerFunc(ws.ListClients)
	log.WithField("listen-addr", ws.ListenAddr).Info("starting HTTP server")
	if dready, err := daemon.SdNotify(false, daemon.SdNotifyReady); !dready {
		if err != nil {
			log.WithError(err).Error("unable to notify systemd of service start due to error")
		} else {
			log.Debug("unable to notify systemd of service start but who cares")
		}
	}
	if wdt, err := daemon.SdWatchdogEnabled(false); wdt != 0 {
		go ws.runSystemdWatchdog(wdt)
	} else {
		if err != nil {
			log.WithError(err).Error("systemd watchdog issue")
		} else {
			log.Debug("systemd watchdog not enabled")
		}
	}
	return http.ListenAndServe(ws.ListenAddr, r)
}
