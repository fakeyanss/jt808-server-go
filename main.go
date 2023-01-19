package main

import (
	"github.com/fakeYanss/jt808-server-go/internal/server"
	"github.com/fakeYanss/jt808-server-go/pkg/log"
)

const (
	TCPPort = "8080"
	UDPPort = "8081"
)

func main() {
	log.Init()

	serv := server.NewTcpServer()
	serv.Listen(":" + TCPPort)
	serv.Start()
}
