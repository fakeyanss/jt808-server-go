package main

import (
	"github.com/fakeYanss/jt808-server-go/internal/server"
	"github.com/fakeYanss/jt808-server-go/pkg/logger"
)

const (
	TCPPort = "8080"
	UDPPort = "8081"
)

const (
	LogDir  = "./logs/" // todo: read from configuration
	LogFile = "jt808-server-go.log"
)

func main() {
	logger.Init(LogDir, LogFile)

	serv := server.NewTcpServer()
	serv.Listen(":" + TCPPort)
	serv.Start()
}
