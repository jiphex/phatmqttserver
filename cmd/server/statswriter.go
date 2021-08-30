package main

import "net/http"

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
