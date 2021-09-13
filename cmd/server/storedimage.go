package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"io/ioutil"
	"mime"
	"time"

	log "github.com/sirupsen/logrus"

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
	simgsize := fmt.Sprintf("%d 𝗑 %d", imc.Width, imc.Height)
	if imc.Width == 212 && imc.Height == 104 {
		log.WithFields(log.Fields{
			"imgsize": simgsize,
			"imgdata": len(imageData),
		}).Debug("processing image upload")
	} else {
		log.WithFields(log.Fields{
			"problem": "incorrect-size",
			"imgsize": simgsize,
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
