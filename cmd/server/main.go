package main

import (
	"bytes"
	"context"
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
	image  []byte
	Type   string
	Stored time.Time
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
		log.WithField("problem", "incorrect-size").Warn("bad image")
		return nil, fmt.Errorf("bad image size")
	}
	log.WithField("format", f).Info("decoded image OK")
	if err != nil {
		log.WithError(err).Error("unable to read image")
		return nil, fmt.Errorf("unable to copy image (but imagedecodeconfig ok)")
	}
	if convert {
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
		image:  imageData,
		Type:   mime.TypeByExtension(fmt.Sprintf(".%s", f)),
		Stored: time.Now(),
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
	u := strings.Join([]string{ws.ExternalURL, "image"}, "/")
	ws.client.Publish("phat/image", byte(1), false, u).Wait()
	log.Info("published")
	return nil
}

func (ws *WatcherServer) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	var err error
	switch req.Method {
	case http.MethodPut:
		if strings.HasPrefix(req.Header.Get("Content-Type"), "image/") {
			rw.WriteHeader(http.StatusCreated)
			ws.lastimg, err = CreateImage(req.Body, true)
			if err != nil {
				rw.WriteHeader(http.StatusNotAcceptable)
				fmt.Fprintf(rw, "ERROR: %s", err.Error())
			}
			log.WithFields(log.Fields{
				"size":   len(ws.lastimg.image),
				"format": ws.lastimg.Type,
			}).Info("stored image")
			ws.publishPost()
			return
		} else {
			rw.WriteHeader(http.StatusNotAcceptable)
			fmt.Fprintf(rw, "ERROR: Content-type not image")
			return
		}
	case http.MethodGet:
		if ws.lastimg == nil {
			rw.WriteHeader(http.StatusNotFound)
			return
		} else {
			log.WithField("format", ws.lastimg.Type).Info("GET request, serving cached image")
			rw.Header().Add("Content-Type", ws.lastimg.Type)
			// wb, err := rw.Write(ws.lastimg.image)
			http.ServeContent(rw, req, "image.png", ws.lastimg.Stored, bytes.NewReader(ws.lastimg.image))
			// if err != nil {
			// 	log.WithError(err).Error("error during image write")
			// 	return
			// }
			// log.WithFields(log.Fields{
			// 	"bytes-wrote": wb,
			// }).Info("wrote image to requestor")
			// return
		}
	}
}

func (ws *WatcherServer) Run(ctx context.Context) error {
	ws.client = mqtt.NewClient(ws.mqttOpts())
	if token := ws.client.Connect(); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return http.ListenAndServe(ws.ListenAddr, ws)
}

func main() {
	app := cli.App{
		Name: "phatmqttserver",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "listen",
				Value: "[::]:39391",
			},
			&cli.StringFlag{
				Name:  "broker",
				Value: "tcp://10.100.100.210:1883",
			},
			&cli.StringFlag{
				Name:  "ext-host",
				Value: "http://10.100.100.148:39391",
			},
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
	app.Run(os.Args)
}
