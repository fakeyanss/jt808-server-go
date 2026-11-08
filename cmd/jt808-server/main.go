package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/alarmserver"
	"github.com/fakeyanss/jt808-server-go/internal/api"
	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
	"github.com/fakeyanss/jt808-server-go/internal/server"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/fakeyanss/jt808-server-go/pkg/logger"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

func main() {
	routines.Recover()

	var cfgPath string
	flag.StringVar(&cfgPath, "c", config.DefaultServConfKey, "config file path")
	flag.Parse()
	fmt.Printf("Start with configuration %v\n", cfgPath)
	cfg := config.Load(cfgPath)

	logConfig := cfg.ParseLogConf()
	log.Logger = *logger.Configure(logConfig).Logger

	if err := cfg.ValidateRetention(); err != nil {
		log.Error().Err(err).Msg("invalid retention configuration")
		os.Exit(1)
	}

	storageCfg := storage.FromConfig(cfg)
	backend, storageErr := storage.Init(storageCfg)
	if storageErr != nil {
		log.Error().Err(storageErr).Msg("Fail to initialize storage backend")
		os.Exit(1)
	}
	log.Info().Str("backend", backend).Msg("Storage backend initialized")

	var hooks action.Hooks = action.Noop{}
	if cfg.Server.Action != nil && cfg.Server.Action.HTTP != nil && cfg.Server.Action.HTTP.URL != "" {
		hooks = action.NewHTTPCallback(cfg.Server.Action.HTTP.URL, time.Duration(cfg.Server.Action.HTTP.TimeoutMS)*time.Millisecond)
		log.Info().Str("url", cfg.Server.Action.HTTP.URL).Msg("Action HTTP callback enabled")
	}

	if cfg.Server.Banner.Enable {
		bannerBytes, err := os.ReadFile(cfg.Server.Banner.BannerPath)
		var banner string
		if err != nil {
			banner = config.BannerText
		} else {
			banner = string(bannerBytes)
		}
		fmt.Println(banner)
	}

	alarmOptions, alarmEnabled, alarmErr := cfg.ParseAlarmServer()
	if alarmErr != nil {
		log.Error().Err(alarmErr).Msg("Invalid alarmServer configuration")
		os.Exit(1)
	}

	serv := server.NewTCPServer()
	serv.SetReliability(time.Duration(cfg.Server.AckTimeoutMS)*time.Millisecond, cfg.Server.MaxRetransmissions)
	if alarmOptions.AutoUpload {
		hooks = alarmserver.NewUploadTrigger(hooks, alarmserver.UploadTriggerOptions{
			IP:      alarmOptions.UploadIP,
			TCPPort: alarmOptions.Port,
			UDPPort: alarmOptions.UDPPort,
		}, func(phone string, msg *model.Msg9208) error {
			ctx := context.Background()
			device, err := storage.Default().Devices.GetByPhone(ctx, phone)
			if err != nil {
				return fmt.Errorf("device is offline: %w", err)
			}
			session, err := runtime.Default().Conn.Get(ctx, device.SessionID)
			if err != nil {
				return fmt.Errorf("session is unavailable: %w", err)
			}
			msg.Header = model.GenMsgHeader(device, 0x9208, session.GetNextSerialNum())
			serv.Send(session.ID, msg)
			return nil
		})
		log.Info().Str("ip", alarmOptions.UploadIP).Uint16("tcp_port", alarmOptions.Port).
			Uint16("udp_port", alarmOptions.UDPPort).
			Msg("Automatic 0x9208 attachment upload trigger enabled")
	}
	serv.SetHooks(hooks)
	addr := ":" + cfg.Server.Port.TCPPort
	err := serv.Listen(addr)
	if err != nil {
		log.Error().Err(err).Str("addr", addr).Msg("Fail to listen tcp addr")
		os.Exit(1)
	}
	routines.GoSafe(func() { serv.Start() })
	if cfg.Server.Port.UDPPort != "" {
		udpServ := server.NewUDPServer()
		udpServ.SetHooks(hooks)
		udpServ.SetReliability(time.Duration(cfg.Server.AckTimeoutMS)*time.Millisecond, cfg.Server.MaxRetransmissions)
		udpAddr := ":" + cfg.Server.Port.UDPPort
		if err := udpServ.Listen(udpAddr); err != nil {
			log.Error().Err(err).Str("addr", udpAddr).Msg("Fail to listen udp addr")
			os.Exit(1)
		}
		routines.GoSafe(func() { udpServ.Start() })
	}

	var alarmServ *alarmserver.Server
	if alarmEnabled {
		alarmServ = alarmserver.NewServer(alarmserver.Config{
			Dir:           alarmOptions.Dir,
			MaxFileSize:   alarmOptions.MaxFileSize,
			MaxConcurrent: alarmOptions.MaxConcurrent,
			CompletedTTL:  alarmOptions.CompletedTTL,
		})
		// TCP 与 UDP 各自独立监听，0x9208 通告的端口即这里的监听端口。
		if alarmOptions.Port != 0 {
			alarmAddr := fmt.Sprintf(":%d", alarmOptions.Port)
			if err := alarmServ.Listen(alarmAddr); err != nil {
				log.Error().Err(err).Str("addr", alarmAddr).Msg("Fail to listen alarm server tcp addr")
				os.Exit(1)
			}
		}
		if alarmOptions.UDPPort != 0 {
			alarmUDPAddr := fmt.Sprintf(":%d", alarmOptions.UDPPort)
			if err := alarmServ.ListenUDP(alarmUDPAddr); err != nil {
				log.Error().Err(err).Str("addr", alarmUDPAddr).Msg("Fail to listen alarm server udp addr")
				os.Exit(1)
			}
		}
		routines.GoSafe(func() { alarmServ.Start() })
	}

	routines.GoSafe(func() { api.Run(serv, cfg, alarmServ) })

	// 运行时状态只存在于进程内，进程退出即自然归零；数据库无需、也不应被批量重置。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Info().Str("signal", sig.String()).Msg("Shutting down")

	if alarmServ != nil {
		alarmServ.Stop()
	}
	if err := storage.Close(); err != nil {
		log.Warn().Err(err).Msg("Fail to close storage backend")
	}
}
