package main

import (
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

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
