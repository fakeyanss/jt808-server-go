package main

import (
	"github.com/rs/zerolog/log"

	"github.com/fakeYanss/jt808-server-go/pkg/logger"
)

const (
	LogDir  = "./logs/" // todo: read from configuration
	LogFile = "jt808-client-go.log"
)

func main() {
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
}
