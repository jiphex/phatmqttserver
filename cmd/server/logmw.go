package main

import (
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

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
