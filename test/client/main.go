package main

import (
	"os"

	"github.com/rs/zerolog/log"

	"github.com/fakeYanss/jt808-server-go/internal/client"
	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeYanss/jt808-server-go/internal/storage"
	"github.com/fakeYanss/jt808-server-go/pkg/logger"
	"github.com/fakeYanss/jt808-server-go/test/datagen"
)

const (
	logDir  = "./logs/" // todo: read from configuration
	logFile = "jt808-client-go.log"
	addr    = "localhost:8080"
)

func main() {
	logConfig := &logger.Config{
		ConsoleLoggingEnabled: true,
		EncodeLogsAsJSON:      false,
		FileLoggingEnabled:    true,
		LogLevel:              0,
		Directory:             logDir,
		Filename:              logFile,
		MaxSize:               5,
		MaxBackups:            128,
		MaxAge:                3,
	}
	log.Logger = *logger.Configure(logConfig).Logger

	cli := client.NewTCPClient()
	err := cli.Dial(addr)
	if err != nil {
		log.Error().
			Err(err).
			Str("addr", addr).
			Msg("Fail to dial tcp addr")
		os.Exit(1)
	}
	defer cli.Stop()

	cache := storage.GetDeviceCache()
	device := datagen.GenDevice()
	device.SessionID = cli.Session.ID
	device.TransProto = model.TCPProto
	device.Conn = cli.Session.Conn
	cache.CacheDevice(device)

	go register(cli)

	cli.Start()
}

func register(cli *client.TCPClient) {
	msg := datagen.GenMsg0100()
	cli.Send(msg)
}
