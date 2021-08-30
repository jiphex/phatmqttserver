package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"github.com/jiphex/phatmqttserver/internal/pkg/phat"
)

type StoredImage struct {
	Image    []byte
	Type     string
	StoredAt time.Time
}

func (si *StoredImage) ETag() string {
	hash := sha256.New()
	hash.Write(si.Image)
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func (si *StoredImage) JSONRepresentation(url string) ([]byte, error) {
	type imageObject struct {
		URL  string `json:"url"`
		Hash string `json:"hash"`
	}
	iobj := imageObject{
		URL:  url,
		Hash: si.ETag(),
	}
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	err := enc.Encode(iobj)
	return buf.Bytes(), err
}

func CreateImage(from io.Reader, convert bool) (*StoredImage, error) {
	imageData, err := ioutil.ReadAll(from)
	if err != nil {
		return nil, err
	}
	imc, f, err := image.DecodeConfig(bytes.NewReader(imageData))
	if err != nil {
		return nil, fmt.Errorf("undecodable image")
	}
	if imc.Width != 212 || imc.Height != 104 {
		simgsize := fmt.Sprintf("%dx%d", imc.Width, imc.Height)
		log.WithFields(log.Fields{
			"problem": "incorrect-size",
			"badsize": simgsize,
		}).Error("bad image size")
		return nil, fmt.Errorf("bad image size: %s", simgsize)
	}
	log.WithField("format", f).Info("decoded image OK")
	if err != nil {
		log.WithError(err).Error("unable to read image")
		return nil, fmt.Errorf("unable to copy image (but imagedecodeconfig ok)")
	}
	if convert {
		log.Debug("converting image to target palette")
		// set the palette
		img, _, err := image.Decode(bytes.NewReader(imageData))
		if err != nil {
			log.WithError(err).Error("unable to decode image")
			return nil, err
		}
		out := image.NewPaletted(img.Bounds(), phat.Palette)
		draw.Draw(out, img.Bounds(), img, img.Bounds().Min, draw.Src)
		buf := new(bytes.Buffer)
		png.Encode(buf, out)
		imageData = buf.Bytes()
	}
	return &StoredImage{
		Image:    imageData,
		Type:     mime.TypeByExtension(fmt.Sprintf(".%s", f)),
		StoredAt: time.Now(),
	}, nil
}

type WatcherServer struct {
	client  mqtt.Client
	lastimg *StoredImage

	Dir         string
	ListenAddr  string
	ExternalURL string
	Broker      string
}

type ClientStatusStatus string

const (
	ALIVE    ClientStatusStatus = "ALIVE"
	DEAD     ClientStatusStatus = "DEAD"
	SHUTDOWN ClientStatusStatus = "SHUTDOWN"
)

type ClientStatus struct {
	Status   ClientStatusStatus `json:"status"`
	LastSeen time.Time          `json:"lastSeen"`
}

var (
	imgMutex    = &sync.RWMutex{}
	clientMutex = &sync.RWMutex{}
	clients     = map[string]ClientStatus{}
)

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

type StatsWriter struct {
	http.ResponseWriter
	BytesWritten int
	WroteStatus  int
}

func (sw *StatsWriter) WriteHeader(statusCode int) {
	sw.WroteStatus = statusCode
	sw.ResponseWriter.WriteHeader(statusCode)
}

func (sw *StatsWriter) Write(p []byte) (n int, err error) {
	n, err = sw.ResponseWriter.Write(p)
	sw.BytesWritten += n
	return n, err
}

func logMw(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		sw := &StatsWriter{
			ResponseWriter: w,
		}
		tStart := time.Now()
		next.ServeHTTP(sw, req)
		log.WithFields(log.Fields{
			"client-addr":   req.RemoteAddr,
			"response-size": sw.BytesWritten,
			"code":          sw.WroteStatus,
			"duration":      time.Since(tStart),
		}).Infof("%s %s", req.Method, req.URL.Path)
	})
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

func main() {
	app := cli.App{
		Name: "phatmqttserver",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "verbose",
				Value:   false,
				Aliases: []string{"v"},
			},
			&cli.StringFlag{
				Name:    "listen",
				Value:   "[::]:39391",
				Aliases: []string{"l"},
				EnvVars: []string{"HTTP_LISTEN"},
			},
			&cli.StringFlag{
				Name: "broker",
				// Value:   "tcp://10.100.100.210:1883",
				Value:   "tcp://127.0.0.1:1883",
				Aliases: []string{"b"},
				EnvVars: []string{"MQTT_BROKER"},
			},
			&cli.StringFlag{
				Name: "ext-host",
				// Value:   "http://10.100.100.148:39391",
				Value:   "http://127.0.0.1:39391",
				Aliases: []string{"x"},
				EnvVars: []string{"EXTERNAL_ADDR"},
			},
		},
		Before: func(cc *cli.Context) error {
			if cc.Bool("verbose") {
				log.SetLevel(log.DebugLevel)
			}
			return nil
		},
		Action: func(cc *cli.Context) error {
			ws := &WatcherServer{
				ListenAddr:  cc.String("listen"),
				Broker:      cc.String("broker"),
				ExternalURL: cc.String("ext-host"),
			}
			return ws.Run(cc.Context)
		},
	}
	if err := app.Run(os.Args); err != nil {
		log.WithError(err).Fatal("error returned")
	}
}
