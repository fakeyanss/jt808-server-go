package main

import (
	"flag"
	"os"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/client"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/fakeyanss/jt808-server-go/pkg/logger"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
	"github.com/fakeyanss/jt808-server-go/test/datagen"
)

const (
	logDir  = "./logs/" // todo: read from configuration
	logFile = "jt808-client-go.log"
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

	var addr string
	flag.StringVar(&addr, "addr", "localhost:8080", "set server addr")
	flag.Parse()

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

	done := make(chan struct{})
	routines.GoSafe(func() {
		cli.Start()
		done <- struct{}{}
	})

	buildDevice(cli)
	register(cli)

	go keepalive(cli)
	go reportLocation(cli)

	for {
		select {
		case <-done:
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func buildDevice(cli *client.TCPClient) {
	cache := storage.GetDeviceCache()
	device := datagen.GenDevice()
	device.SessionID = cli.Session.ID
	device.TransProto = model.TCPProto
	device.Conn = cli.Session.Conn
	cache.CacheDevice(device)
}

func register(cli *client.TCPClient) {
	msg := datagen.GenMsg0100()
	cli.Send(msg)
}

func keepalive(cli *client.TCPClient) {
	msg := datagen.GenMsg0002()
	for {
		cli.Send(msg)
		time.Sleep(10 * time.Second)
	}
}

func reportLocation(cli *client.TCPClient) {
	for {
		msg := datagen.GenMsg0200()
		cli.Send(msg)
		time.Sleep(30 * time.Second)
	}
}
