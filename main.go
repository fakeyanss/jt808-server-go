package main

import (
	"github.com/rs/zerolog/log"

	"github.com/fakeYanss/jt808-server-go/internal/server"
	"github.com/fakeYanss/jt808-server-go/pkg/logger"
	"github.com/fakeYanss/jt808-server-go/pkg/routines"
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
	routines.Recover()

	logConfig := &logger.Config{
		ConsoleLoggingEnabled: true,
		EncodeLogsAsJSON:      false,
		FileLoggingEnabled:    true,
		LogLevel:              0,
		Directory:             LogDir,
		Filename:              LogFile,
		MaxSize:               5,
		MaxBackups:            128,
		MaxAge:                3,
	}
	log.Logger = *logger.Configure(logConfig).Logger

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
