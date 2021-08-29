package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"

	"github.com/jiphex/phatmqttserver/pkg/gen"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

const (
	flagVerbose = "verbose"
)

var (
	phatPallete = color.Palette{
		color.Black,
		color.White,
		color.RGBA{255, 0, 0, 255},
	}
)

func main() {
	app := &cli.App{
		Commands: []*cli.Command{
			{
				Name:  "convert",
				Usage: "convert an appropriately-sized image to the phatPi image pallete",
				Flags: []cli.Flag{
					&cli.PathFlag{
						Name:    "output",
						Aliases: []string{"o"},
						Usage:   "output path",
					},
				},
			},
			{
				Name:  "generate",
				Usage: "generate an image from Internet data",
				Action: func(cc *cli.Context) error {
					gen.Draw()
					return nil
				},
			},
		},
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    flagVerbose,
				Value:   false,
				Usage:   "enable verbose logging",
				Aliases: []string{"v"},
			},
		},
		Before: func(cc *cli.Context) error {
			if cc.Bool(flagVerbose) {
				log.SetLevel(log.DebugLevel)
			}
			return nil
		},
	}
	app.Run(os.Args)
}

func convertImageCmd(cc *cli.Context) error {
	imgfile := os.Args[1]
	f, err := os.Open(imgfile)
	if err != nil {
		log.WithError(err).Fatal("unable to open file")
	}
	img, err := png.Decode(f)
	if err != nil {
		log.WithError(err).Fatal("unable to load png")
	}
	fmt.Printf("image is %+v", img.ColorModel())
	out := image.NewPaletted(img.Bounds(), phatPallete)
	draw.Draw(out, img.Bounds(), img, img.Bounds().Min, draw.Src)
	fout, _ := os.Create("out.png")
	return png.Encode(fout, out)
}
