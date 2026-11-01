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
	"github.com/fakeyanss/jt808-server-go/internal/api"
	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/protocol"
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

	storageCfg := storage.FromConfig(cfg)
	backend, storageErr := storage.Init(storageCfg)
	if storageErr != nil {
		log.Error().Err(storageErr).Msg("Fail to initialize storage backend")
		os.Exit(1)
	}
	log.Info().Str("backend", backend).Msg("Storage backend initialized")

	// 心跳落库节流：0 表示逐条写库（缺省）；若开启，应小于设备保活周期（当前 1 分钟）。
	protocol.SetHeartbeatPersistInterval(time.Duration(cfg.Server.HeartbeatPersistIntervalMS) * time.Millisecond)

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

	serv := server.NewTCPServer()
	serv.SetHooks(hooks)
	serv.SetReliability(time.Duration(cfg.Server.AckTimeoutMS)*time.Millisecond, cfg.Server.MaxRetransmissions)
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

	routines.GoSafe(func() { api.Run(serv, cfg) })

	// 单机优雅停机：进程退出前清空运行时投影，避免下次启动读到残留的在线状态。
	// 分布式形态不能用整体重置（会覆盖其它节点），改由 Redis 运行时缓存 TTL 扫描（§7.4）。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Info().Str("signal", sig.String()).Msg("Shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := storage.ResetRuntime(ctx); err != nil {
		log.Warn().Err(err).Msg("Fail to reset device runtime on shutdown")
	}
	if err := storage.Close(); err != nil {
		log.Warn().Err(err).Msg("Fail to close storage backend")
	}
}
