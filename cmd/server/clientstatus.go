package main

import "time"

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
