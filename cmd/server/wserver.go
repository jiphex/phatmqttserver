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

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
)

var (
	imgMutex    = &sync.RWMutex{}
	clientMutex = &sync.RWMutex{}
	clients     = map[string]ClientStatus{}
)

type WatcherServer struct {
	client  mqtt.Client
	lastimg *StoredImage

	Dir         string
	ListenAddr  string
	ExternalURL string
	Broker      string
}

func (ws *WatcherServer) clientUpdate(cl mqtt.Client, msg mqtt.Message) {
	topicparts := strings.SplitN(msg.Topic(), "/", 3)
	log.WithFields(log.Fields{
		"client": topicparts[2],
		"status": string(msg.Payload()),
	}).Info("client status update")
	clientMutex.Lock()
	defer clientMutex.Unlock()
	clients[topicparts[2]] = ClientStatus{
		Status:   ClientStatusStatus(msg.Payload()),
		LastSeen: time.Now(),
	}
}

func (ws *WatcherServer) mqttOpts() *mqtt.ClientOptions {
	opts := &mqtt.ClientOptions{}
	opts.SetClientID("mqttphatserver")
	opts.AddBroker(ws.Broker)
	opts.OnConnect = func(client mqtt.Client) {
		log.WithField("broker", ws.Broker).Info("MQTT broker connected")
		client.Subscribe("phat/client/+", byte(1), ws.clientUpdate).Wait()
	}
	return opts
}

func (ws *WatcherServer) publishPost() error {
	imgMutex.RLock()
	defer imgMutex.RUnlock()
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
	imgMutex.RLock()
	defer imgMutex.RUnlock()
	if ws.lastimg == nil {
		log.Warn("GET request but no image ready for download")
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(rw, "No image cached yet\n")
		return
	} else {
		log.WithField("format", ws.lastimg.Type).Info("GET request, serving cached image")
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
	clientMutex.RLock()
	defer clientMutex.RUnlock()
	output := clientsListOutput{
		Clients: clients,
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
		imgMutex.Lock()
		defer imgMutex.Unlock()
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
func (ws *WatcherServer) Run(ctx context.Context) error {
	ws.client = mqtt.NewClient(ws.mqttOpts())
	if token := ws.client.Connect(); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	go ws.runPeriodicPublisher(10 * 60 * time.Second)
	r := mux.NewRouter()
	r.Use(logMw)
	r.Methods(http.MethodPut).HandlerFunc(ws.ImageUpload)
	getroutes := r.Methods(http.MethodGet).Subrouter()
	getroutes.Path("/clients").HandlerFunc(ws.ListClients)
	getroutes.Path("/").HandlerFunc(ws.ImageDownload)
	log.WithField("listen-addr", ws.ListenAddr).Info("starting HTTP server")
	return http.ListenAndServe(ws.ListenAddr, r)
}
