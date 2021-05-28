all: phatmqttserver imgtool

phatmqttserver: cmd/server/**.go go.mod go.sum
	go build -o $@ ./cmd/server

imgtool: cmd/imgtool/**.go go.mod go.sum
	go build -o $@ ./cmd/imgtool