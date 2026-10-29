package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
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
	retryMaxCnt           = 6
	retryIntervalInSecond = 10
)

type (
	DeviceConfCtxKey struct{}

	DeviceGeoConfCtxKey struct{}

	DevicePhoneCtxKey struct{}
)

func buildDevice(ctx context.Context, cli *client.TCPClient) *model.Device {
	deviceConf := ctx.Value(DeviceConfCtxKey{}).(*config.DeviceConf)
	cache := storage.GetDeviceCache()
	device := datagen.GenDevice(deviceConf)
	device.SessionID = cli.Session.ID
	device.TransProto = cli.Session.GetTransProto()
	device.Conn = cli.Session.Conn
	cache.CacheDevice(device)
	return device
}

func getDevice(ctx context.Context) *model.Device {
	phone := ctx.Value(DevicePhoneCtxKey{}).(string)
	cache := storage.GetDeviceCache()
	device, err := cache.GetDeviceByPhone(phone)
	if err != nil {
		log.Fatal().Err(err).Str("phone", phone).Msg("Fail to find device cache")
	}
	return device
}

func register(ctx context.Context, cli *client.TCPClient, fragmentSize int, reverseFragments bool, dropFragment uint16, faultFrame string) error {
	device := getDevice(ctx)
	deviceConf := ctx.Value(DeviceConfCtxKey{}).(*config.DeviceConf)
	msg := datagen.GenMsg0100(deviceConf, device)
	if faultFrame != "" {
		return cli.SendMalformedFrame(msg, faultFrame)
	}
	if fragmentSize > 0 {
		return cli.SendFragmentsWithFault(msg, fragmentSize, reverseFragments, dropFragment)
	}
	cli.Send(msg)
	return nil
}

func keepalive(ctx context.Context, cli *client.TCPClient) {
	device := getDevice(ctx)
	deviceConf := ctx.Value(DeviceConfCtxKey{}).(*config.DeviceConf)
	msg := datagen.GenMsg0002(device)

	for {
		cli.Send(msg)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(deviceConf.Keepalive) * time.Second):
		}
	}
}

func buildDeviceGeo(ctx context.Context) {
	device := getDevice(ctx)
	deviceGeoConf := ctx.Value(DeviceGeoConfCtxKey{}).(*config.DeviceGeoConf)
	deivceGeo := datagen.GenDeviceGeo(deviceGeoConf, device)
	geoCache := storage.GetGeoCache()
	rb := geoCache.GetGeoRingByPhone(device.Phone)
	rb.Write(deivceGeo)
}

func reportLocation(ctx context.Context, cli *client.TCPClient) {
	deviceGeoConf := ctx.Value(DeviceGeoConfCtxKey{}).(*config.DeviceGeoConf)
	geoCache := storage.GetGeoCache()
	tracker := storage.GetLocationTrackingCache()
	device := getDevice(ctx)
	fallback := time.Duration(deviceGeoConf.LocationReportInterval) * time.Second
	sendLocation := func() {
		deviceGeo, err := geoCache.GetGeoLatestByPhone(device.Phone)
		if err == nil {
			cli.Send(datagen.GenMsg0200(deviceGeoConf, device, deviceGeo))
		}
	}
	sendLocation()
	timer := time.NewTimer(tracker.Interval(device.Phone, fallback))
	defer timer.Stop()
	changed := tracker.Changed(device.Phone)
	for {
		select {
		case <-ctx.Done():
			return
		case <-changed:
			changed = tracker.Changed(device.Phone)
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(tracker.Interval(device.Phone, fallback))
			sendLocation()
		case <-timer.C:
			sendLocation()
			timer.Reset(tracker.Interval(device.Phone, fallback))
		}
	}
}

func dialAndSend(cfg *config.Config, cliWg *sync.WaitGroup, fragmentSize int, reverseFragments bool, dropFragment uint16, faultFrame string, industryReports bool) {
	cli := client.NewTCPClient()
	addr := cfg.Client.Conn.RemoteAddr
	for retry := 1; ; retry++ {
		transport := model.TransportProtocol(cfg.Client.Device.TransProto)
		if transport == "" {
			transport = model.TCPProto
		}
		err := cli.DialTransport(addr, transport)
		if err == nil {
			cli.Session.AckTimeout = time.Duration(cfg.Client.AckTimeoutMS) * time.Millisecond
			cli.Session.MaxRetries = cfg.Client.MaxRetransmissions
			break
		}
		errDialMsg := "Fail to dial to the tcp addr"
		if retry > retryMaxCnt {
			log.Error().Err(err).Str("addr", addr).Msgf("%s, exit", errDialMsg)
			os.Exit(1)
		}
		log.Error().Err(err).Str("addr", addr).Msgf("%s, retry", errDialMsg)
		time.Sleep(retryIntervalInSecond * time.Second)
	}
	routines.GoSafe(func() {
		defer cliWg.Done()
		cli.Start()
	})

	routines.GoSafe(func() {
		ctx := context.WithValue(context.Background(), DeviceConfCtxKey{}, cfg.Client.Device)
		d := buildDevice(ctx, cli)
		ctx = context.WithValue(ctx, DevicePhoneCtxKey{}, d.Phone)
		ctx = context.WithValue(ctx, DeviceGeoConfCtxKey{}, cfg.Client.DeviceGeo)
		buildDeviceGeo(ctx)

		var wg sync.WaitGroup
		wg.Add(1)
		if err := register(ctx, cli, fragmentSize, reverseFragments, dropFragment, faultFrame); err != nil {
			log.Error().Err(err).Msg("Failed to send registration message")
			cli.Stop()
			return
		}

		routines.GoSafe(func() {
			// device status checker
			for {
				cache := storage.GetDeviceCache()
				renewDevice, _ := cache.GetDeviceByPhone(d.Phone)
				if renewDevice.Status == model.DeviceStatusOnline {
					wg.Done()
					return
				}
				time.Sleep(time.Second)
			}
		})

		// should wait for register success, and stop after register failed for a while
		routines.GoSafe(func() {
			wg.Wait()
			keepalive(ctx, cli)
		})
		routines.GoSafe(func() {
			wg.Wait()
			reportLocation(ctx, cli)
		})
		if industryReports {
			routines.GoSafe(func() {
				wg.Wait()
				for _, msg := range []model.JT808Msg{datagen.GenMsg0702(d), datagen.GenMsg0705(d), datagen.GenMsg0700(d), datagen.GenMsg0800(d), datagen.GenMsg0801(d)} {
					storage.GetIndustryCache().Record(msg)
					cli.Send(msg)
				}
			})
		}
	})
}

func main() {
	var cfgPath string
	var fragmentSize int
	var reverseFragments bool
	var dropFragment uint
	var faultFrame string
	var industryReports bool
	flag.StringVar(&cfgPath, "c", config.DefaultCliConfKey, "config file path")
	flag.IntVar(&fragmentSize, "fragment-size", 0, "send registration as JT808 fragments with this body size (0 disables fragmentation)")
	flag.BoolVar(&reverseFragments, "reverse-fragments", false, "send fragment indexes in reverse order")
	flag.UintVar(&dropFragment, "drop-fragment", 0, "omit this 1-based fragment index initially and wait for a 0x8003 retransmission request (0 disables dropping)")
	flag.StringVar(&faultFrame, "fault-frame", "", "send a malformed frame before registration: bad-checksum, bad-escape, or oversize")
	flag.BoolVar(&industryReports, "industry-reports", false, "send one driver, CAN, driving-recorder, multimedia-event, and multimedia-upload test report after authentication")
	flag.Parse()
	fmt.Printf("Start with configuration %v\n", cfgPath)
	cfg := config.Load(cfgPath)
	if dropFragment > 65535 {
		fmt.Fprintln(os.Stderr, "drop-fragment must be between 0 and 65535")
		os.Exit(2)
	}
	if dropFragment > 0 && fragmentSize == 0 {
		fmt.Fprintln(os.Stderr, "drop-fragment requires a positive fragment-size")
		os.Exit(2)
	}

	logCfg := cfg.ParseLogConf()
	log.Logger = *logger.Configure(logCfg).Logger

	var cliWg sync.WaitGroup
	cliWg.Add(cfg.Client.Concurrency)

	for i := 0; i < cfg.Client.Concurrency; i++ {
		dialAndSend(cfg, &cliWg, fragmentSize, reverseFragments, uint16(dropFragment), faultFrame, industryReports)
	}

	cliWg.Wait()
}
