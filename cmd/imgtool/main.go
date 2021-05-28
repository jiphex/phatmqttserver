package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"

	log "github.com/sirupsen/logrus"
)

var (
	phatPallete = color.Palette{
		color.Black,
		color.White,
		color.RGBA{255, 0, 0, 255},
	}
)

func main() {
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
	png.Encode(fout, out)
}
