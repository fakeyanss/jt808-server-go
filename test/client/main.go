package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/client"
	"github.com/fakeyanss/jt808-server-go/internal/config"
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
	var cfgPath string
	flag.StringVar(&cfgPath, "c", "configs/default.toml", "config file path")
	flag.Parse()
	fmt.Printf("Start with configuration %v\n", cfgPath)
	cfg := config.Load(cfgPath)
	fmt.Printf("Load configuration: %+v\n", cfg)

	logCfg := cfg.ParseLogConf()
	log.Logger = *logger.Configure(logCfg).Logger

	addr := cfg.Client.Conn.RemoteAddr
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

	for i := 0; i < cfg.Client.Concurrency; i++ {
		routines.GoSafe(func() {
			d := buildDevice(cli)
			ctx := context.WithValue(context.Background(), model.DeviceCtxKey{}, d.PhoneNumber)

			register(ctx, cli)

			time.Sleep(10 * time.Second)

			// todo should wait for register success
			routines.GoSafe(func() { keepalive(ctx, cli) })
			// todo should wait for register success
			routines.GoSafe(func() { reportLocation(ctx, cli, cfg.Client.Device.LocationReportInteval) })
		})
	}

	for {
		select {
		case <-done:
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func buildDevice(cli *client.TCPClient) *model.Device {
	cache := storage.GetDeviceCache()
	device := datagen.GenDevice()
	device.SessionID = cli.Session.ID
	device.TransProto = model.TCPProto
	device.Conn = cli.Session.Conn
	cache.CacheDevice(device)
	return device
}

func getDevice(ctx context.Context) *model.Device {
	phone := ctx.Value(model.DeviceCtxKey{}).(string)
	cache := storage.GetDeviceCache()
	device, err := cache.GetDeviceByPhone(phone)
	if err != nil {
		log.Fatal().Err(err).Str("phone", phone).Msg("Fail to find device cache")
	}
	return device
}

func register(ctx context.Context, cli *client.TCPClient) {
	device := getDevice(ctx)
	msg := datagen.GenMsg0100(device)
	cli.Send(msg)
}

func keepalive(ctx context.Context, cli *client.TCPClient) {
	device := getDevice(ctx)
	msg := datagen.GenMsg0002(device)
	for {
		cli.Send(msg)
		time.Sleep(device.Keepalive)
	}
}

func reportLocation(ctx context.Context, cli *client.TCPClient, interval int) {
	for {
		device := getDevice(ctx)
		msg := datagen.GenMsg0200(device)
		cli.Send(msg)
		time.Sleep(time.Duration(interval) * time.Second)
	}
}
