package main

import (
	"github.com/rs/zerolog/log"

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

	serv := server.NewTCPServer()
	addr := ":" + TCPPort
	err := serv.Listen(addr)
	if err != nil {
		log.Error().
			Err(err).
			Str("addr", addr).
			Msg("Fail to listen addr")
	}
	serv.Start()
}
