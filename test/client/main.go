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
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
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
	device := datagen.GenDevice(deviceConf)
	device.SessionID = cli.Session.ID
	device.TransProto = cli.Session.GetTransProto()
	device.Conn = cli.Session.Conn
	_ = repository.Default().Devices.Upsert(ctx, device)
	return device
}

func getDevice(ctx context.Context) *model.Device {
	phone := ctx.Value(DevicePhoneCtxKey{}).(string)
	device, err := repository.Default().Devices.GetByPhone(ctx, phone)
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
	_ = repository.Default().Geo.Append(ctx, deivceGeo)
}

func reportLocation(ctx context.Context, cli *client.TCPClient) {
	deviceGeoConf := ctx.Value(DeviceGeoConfCtxKey{}).(*config.DeviceGeoConf)
	geo := repository.Default().Geo
	tracker := runtime.Default().Tracking
	device := getDevice(ctx)
	fallback := time.Duration(deviceGeoConf.LocationReportInterval) * time.Second
	sendLocation := func() {
		deviceGeo, err := geo.Latest(ctx, device.Phone)
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

func dialAndSend(cfg *config.Config, cliWg *sync.WaitGroup, fragmentSize int, reverseFragments bool, dropFragment uint16, faultFrame string, industryReports, logout bool) {
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
				renewDevice, _ := repository.Default().Devices.GetByPhone(ctx, d.Phone)
				if renewDevice.Status == model.DeviceStatusOnline {
					wg.Done()
					return
				}
				time.Sleep(time.Second)
			}
		})

		// should wait for register success, and stop after register failed for a while
		if !logout {
			routines.GoSafe(func() {
				wg.Wait()
				keepalive(ctx, cli)
			})
			routines.GoSafe(func() {
				wg.Wait()
				reportLocation(ctx, cli)
			})
		}
		if industryReports {
			routines.GoSafe(func() {
				wg.Wait()
				// Query the platform clock once; the 0x8004 reply is decoded by
				// the client pipeline and needs no local cache record.
				cli.Send(datagen.GenMsg0004(d))
				for _, msg := range []model.JT808Msg{
					datagen.GenMsg0301(d, 1),                                   // 事件上报
					datagen.GenMsg0302(d, 1, 1),                                // 提问应答
					datagen.GenMsg0303(d, 1, 1),                                // 信息点播
					datagen.GenMsg0108(d, 0, 0),                                // 升级结果通知
					datagen.GenMsg0701(d, "模拟电子运单内容"),                          // 电子运单
					datagen.GenMsg0704(d),                                      // 批量定位
					datagen.GenMsg0900(d, 0x0002, []byte{0x11, 0x22}),          // 数据上行透传
					datagen.GenMsg0901(d, []byte("compressed uplink payload")), // 数据压缩上报
					datagen.GenMsg0702(d),                                      // 驾驶员身份识别
					datagen.GenMsg0705(d),                                      // CAN 总线数据
					datagen.GenMsg0700(d),                                      // 行驶记录仪数据
					datagen.GenMsg0800(d),                                      // 多媒体事件
					datagen.GenMsg0801(d),                                      // 多媒体数据
				} {
					repository.Default().Industry.Record(msg)
					cli.Send(msg)
				}
			})
		}
		if logout {
			routines.GoSafe(func() {
				wg.Wait()
				// 注销后服务端清除设备缓存，模拟器随即关闭连接。
				cli.Send(datagen.GenMsg0003(d))
				cli.Stop()
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
	var logout bool
	flag.StringVar(&cfgPath, "c", config.DefaultCliConfKey, "config file path")
	flag.IntVar(&fragmentSize, "fragment-size", 0, "send registration as JT808 fragments with this body size (0 disables fragmentation)")
	flag.BoolVar(&reverseFragments, "reverse-fragments", false, "send fragment indexes in reverse order")
	flag.UintVar(&dropFragment, "drop-fragment", 0, "omit this 1-based fragment index initially and wait for a 0x8003 retransmission request (0 disables dropping)")
	flag.StringVar(&faultFrame, "fault-frame", "", "send a malformed frame before registration: bad-checksum, bad-escape, or oversize")
	flag.BoolVar(&industryReports, "industry-reports", false, "send one of every supported uplink report after authentication")
	flag.BoolVar(&logout, "logout", false, "send a 0x0003 logout after authentication and stop the simulator")
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
		dialAndSend(cfg, &cliWg, fragmentSize, reverseFragments, uint16(dropFragment), faultFrame, industryReports, logout)
	}

	cliWg.Wait()
}
