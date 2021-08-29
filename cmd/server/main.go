package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
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
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

var (
	phatPallete = color.Palette{
		color.Black,
		color.White,
		color.RGBA{255, 0, 0, 255},
	}
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
		out := image.NewPaletted(img.Bounds(), phatPallete)
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

func (ws *WatcherServer) mqttOpts() *mqtt.ClientOptions {
	opts := &mqtt.ClientOptions{}
	opts.SetClientID("mqttphatserver")
	opts.AddBroker(ws.Broker)
	opts.OnConnect = func(client mqtt.Client) {
		log.WithField("broker", ws.Broker).Info("mqtt broker connected")
	}
	return opts
}

func (ws *WatcherServer) publishPost() error {
	imgMutex.RLock()
	defer imgMutex.RUnlock()
	if ws.lastimg != nil {
		u := strings.Join([]string{ws.ExternalURL, "image"}, "/")
		ws.client.Publish("phat/image", byte(1), false, u).Wait()
		log.Info("published")
		return nil
	} else {
		return errors.New("not ready")
	}
}

var (
	imgMutex = &sync.RWMutex{}
)

func (ws *WatcherServer) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	defer func(req *http.Request) {
		log.WithFields(log.Fields{
			"client-addr": req.RemoteAddr,
			"method":      req.Method,
		}).Info(req.URL.Path)
	}(req)
	var err error
	switch req.Method {
	case http.MethodPut:
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
	case http.MethodGet:
		imgMutex.RLock()
		defer imgMutex.RUnlock()
		if ws.lastimg == nil {
			rw.WriteHeader(http.StatusNotFound)
			return
		} else {
			log.WithField("format", ws.lastimg.Type).Info("GET request, serving cached image")
			rw.Header().Add("Content-Type", ws.lastimg.Type)
			rw.Header().Set("ETag", ws.lastimg.ETag())
			// wb, err := rw.Write(ws.lastimg.image)
			http.ServeContent(rw, req, "image.png", ws.lastimg.StoredAt, bytes.NewReader(ws.lastimg.Image))
		}
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
	return http.ListenAndServe(ws.ListenAddr, ws)
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
