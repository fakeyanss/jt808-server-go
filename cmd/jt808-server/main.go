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
	"github.com/fakeyanss/jt808-server-go/internal/adminapi"
	"github.com/fakeyanss/jt808-server-go/internal/alarmserver"
	"github.com/fakeyanss/jt808-server-go/internal/api"
	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/control"
	"github.com/fakeyanss/jt808-server-go/internal/media"
	"github.com/fakeyanss/jt808-server-go/internal/mediaupload"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/retention"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
	"github.com/fakeyanss/jt808-server-go/internal/server"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/fakeyanss/jt808-server-go/internal/uplink"
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
	outboxOptions, outboxErr := cfg.ParseCommandOutbox()
	if outboxErr != nil {
		log.Error().Err(outboxErr).Msg("Invalid commandOutbox configuration")
		os.Exit(1)
	}
	uplinkOptions, uplinkErr := cfg.ParseUplink()
	if uplinkErr != nil {
		log.Error().Err(uplinkErr).Msg("Invalid uplink reliability configuration")
		os.Exit(1)
	}
	uplinkSinkOptions, uplinkSinkErr := cfg.ParseUplinkSink()
	if uplinkSinkErr != nil {
		log.Error().Err(uplinkSinkErr).Msg("Invalid uplink sink configuration")
		os.Exit(1)
	}

	storageCfg := storage.FromConfig(cfg)
	stores, storageErr := storage.Init(storageCfg)
	if storageErr != nil {
		log.Error().Err(storageErr).Msg("Fail to initialize storage backend")
		os.Exit(1)
	}
	log.Info().Str("backend", stores.Backend).Msg("Storage backend initialized")

	// mode 未显式配置时按存储后端归一化：memory 后端默认 memory，SQL 后端默认 durable。
	outboxOptions.Mode = config.ResolveCommandOutboxMode(outboxOptions.Mode, stores.Backend)
	var commandRepo repository.CommandRepository
	switch outboxOptions.Mode {
	case config.CommandOutboxModeOff:
	case config.CommandOutboxModeMemory:
		commandRepo = repository.NewCommandRepository()
	case config.CommandOutboxModeDurable:
		commandRepo = stores.Commands
	}
	log.Info().Str("mode", outboxOptions.Mode).Msg("Command outbox mode resolved")

	// 上行可靠性：at_least_once 分类先完成落库再应答，落库失败由终端重传。
	uplinkPolicy := uplink.NewPolicy(uplinkOptions.Default, uplinkOptions.Location,
		uplinkOptions.BatchLocation, uplinkOptions.Alarm)
	log.Info().
		Str("default", string(uplinkPolicy.Default)).
		Str("location", string(uplinkPolicy.Location)).
		Str("batch_location", string(uplinkPolicy.BatchLocation)).
		Str("alarm", string(uplinkPolicy.Alarm)).
		Msg("Uplink reliability configured")

	deps := protocol.ProcessorDeps{Storage: stores, Runtime: runtime.Default(), Uplink: &uplinkPolicy}

	// 上行出口：默认 local 保持进程内直连处理；http 把业务事件投递给管控。
	// 出口只负责传输，解码、分包与应答仍在协议内核完成。
	var uplinkSink *uplink.HTTPSink
	if uplinkSinkOptions.Mode == config.UplinkSinkHTTP {
		sink, err := uplink.NewHTTPSink(uplink.HTTPSinkOptions{
			URL:         uplinkSinkOptions.HTTP.URL,
			Token:       uplinkSinkOptions.HTTP.Token,
			Timeout:     uplinkSinkOptions.HTTP.Timeout,
			QueueSize:   uplinkSinkOptions.HTTP.QueueSize,
			Workers:     uplinkSinkOptions.HTTP.Workers,
			MaxAttempts: uplinkSinkOptions.HTTP.MaxAttempts,
		})
		if err != nil {
			log.Error().Err(err).Msg("Fail to initialize uplink HTTP sink")
			os.Exit(1)
		}
		uplinkSink = sink
		deps.UplinkSink = sink
		deps.GatewayID = uplinkSinkOptions.HTTP.GatewayID
		log.Info().
			Str("url", uplinkSinkOptions.HTTP.URL).
			Dur("timeout", uplinkSinkOptions.HTTP.Timeout).
			Int("queue_size", uplinkSinkOptions.HTTP.QueueSize).
			Int("workers", uplinkSinkOptions.HTTP.Workers).
			Int("max_attempts", uplinkSinkOptions.HTTP.MaxAttempts).
			Str("gateway_id", uplinkSinkOptions.HTTP.GatewayID).
			Msg("Uplink HTTP sink enabled")
	} else {
		log.Info().Str("mode", uplinkSinkOptions.Mode).Msg("Uplink sink configured")
	}

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
	mediaOptions, mediaEnabled, mediaErr := cfg.ParseMediaServer()
	if mediaErr != nil {
		log.Error().Err(mediaErr).Msg("Invalid mediaServer configuration")
		os.Exit(1)
	}

	// 0x0801 多媒体文件落盘始终启用；集群模式要求该目录位于共享卷上。
	mediaUploadOptions, mediaUploadErr := cfg.ParseMediaUpload()
	if mediaUploadErr != nil {
		log.Error().Err(mediaUploadErr).Msg("Invalid mediaUpload configuration")
		os.Exit(1)
	}
	mediaUploadStore, mediaUploadErr := mediaupload.NewStore(mediaUploadOptions.Dir)
	if mediaUploadErr != nil {
		log.Error().Err(mediaUploadErr).Msg("Fail to initialize media upload store")
		os.Exit(1)
	}
	deps.MediaUpload = mediaUploadStore
	log.Info().Str("dir", mediaUploadStore.Root()).Msg("Media upload store initialized")

	adminOptions, adminEnabled, adminErr := cfg.ParseAdmin()
	if adminErr != nil {
		log.Error().Err(adminErr).Msg("Invalid admin configuration")
		os.Exit(1)
	}
	apiOptions, apiErr := cfg.ParseAPI()
	if apiErr != nil {
		log.Error().Err(apiErr).Msg("Invalid business API configuration")
		os.Exit(1)
	}
	if apiOptions.Token == "" {
		log.Warn().Msg("Business API authentication is disabled; set api.auth.token in production")
	}

	serv := server.NewTCPServer()
	serv.SetDeps(deps)
	serv.SetReliability(time.Duration(cfg.Server.AckTimeoutMS)*time.Millisecond, cfg.Server.MaxRetransmissions)
	metrics := observability.NewMetrics()
	serv.SetMetrics(metrics)

	// durable outbox 归内置管控模块所有：幂等、租约、退避、过期与重投都在这里，
	// 协议层只上报可靠投递结果。off 模式允许命令仓储为空，直接发送。
	outbox, outboxBuildErr := control.NewOutbox(control.OutboxDependencies{
		Commands: commandRepo,
		Devices:  stores.Devices,
		Conn:     deps.Runtime.Conn,
		Metrics:  metrics,
		Send: func(sessionID string, msg model.JT808Msg, serial uint16) error {
			return serv.Send(sessionID, msg, server.WithSerial(serial))
		},
	}, control.OutboxOptions{
		Enabled:                outboxOptions.Mode != config.CommandOutboxModeOff,
		InitialWait:            outboxOptions.InitialWait,
		ExpireAfter:            outboxOptions.ExpireAfter,
		PollInterval:           outboxOptions.PollInterval,
		BatchSize:              outboxOptions.BatchSize,
		MaxAttempts:            outboxOptions.MaxAttempts,
		LeaseDuration:          outboxOptions.LeaseDuration,
		RetryBase:              outboxOptions.RetryBase,
		RetryMax:               outboxOptions.RetryMax,
		RetryOnTerminalFailure: outboxOptions.RetryOnTerminalFailure,
	})
	if outboxBuildErr != nil {
		log.Error().Err(outboxBuildErr).Msg("Fail to create command outbox")
		os.Exit(1)
	}
	// 鉴权提供者是网关向管控查询设备档案的只读契约；standalone 下为进程内实现。
	authProvider := control.NewLocalAuthProvider(stores.Devices)

	// 历史数据定期清理与孤儿文件扫描：默认关闭，只有显式开启且后端支持时才启动。
	// memory 后端的数据随进程结束释放，也没有共享库作为孤儿比对依据，两者都跳过。
	retentionPolicy := cfg.RetentionPolicy()
	cancelRetention := func() {}
	var retentionDones []<-chan struct{}
	var retentionCtx context.Context
	if retentionPolicy.Cleanup.Enabled || retentionPolicy.OrphanFiles.Enabled {
		ctx, cancel := context.WithCancel(context.Background())
		retentionCtx = ctx
		cancelRetention = cancel
	}
	if retentionPolicy.Cleanup.Enabled {
		if stores.Retention == nil {
			log.Warn().Str("backend", stores.Backend).
				Msg("Retention cleanup is enabled but the storage backend does not support it; skipped")
		} else {
			cleaner := retention.NewCleaner(stores.Retention, mediaUploadStore, retention.Config{
				Interval:           retentionPolicy.Cleanup.Interval,
				BatchSize:          retentionPolicy.Cleanup.BatchSize,
				Locations:          retentionPolicy.Locations.MaxAge,
				Alarms:             retentionPolicy.Alarms.MaxAge,
				MediaFiles:         retentionPolicy.MediaFiles.MaxAge,
				MediaCaptures:      retentionPolicy.MediaCaptures.MaxAge,
				MediaSearchResults: retentionPolicy.MediaSearchResults.MaxAge,
			}, metrics)
			retentionDones = append(retentionDones, cleaner.Done())
			routines.GoSafe(func() { cleaner.Run(retentionCtx) })
			log.Info().
				Dur("interval", retentionPolicy.Cleanup.Interval).
				Int("batch_size", retentionPolicy.Cleanup.BatchSize).
				Dur("locations", retentionPolicy.Locations.MaxAge).
				Dur("alarms", retentionPolicy.Alarms.MaxAge).
				Dur("media_files", retentionPolicy.MediaFiles.MaxAge).
				Dur("media_captures", retentionPolicy.MediaCaptures.MaxAge).
				Dur("media_search_results", retentionPolicy.MediaSearchResults.MaxAge).
				Msg("Retention cleanup enabled")
		}
	}
	if retentionPolicy.OrphanFiles.Enabled {
		if stores.Retention == nil || mediaUploadStore == nil {
			log.Warn().Str("backend", stores.Backend).
				Msg("Orphan media scan is enabled but the storage backend does not support it; skipped")
		} else {
			scanner := retention.NewOrphanScanner(stores.Retention, mediaUploadStore, retention.OrphanConfig{
				Enabled:        true,
				Grace:          retentionPolicy.OrphanFiles.Grace,
				Interval:       retentionPolicy.OrphanFiles.Interval,
				MaxFilesPerRun: retentionPolicy.OrphanFiles.MaxFilesPerRun,
				BatchSize:      retentionPolicy.OrphanFiles.BatchSize,
			}, metrics)
			retentionDones = append(retentionDones, scanner.Done())
			routines.GoSafe(func() { scanner.Run(retentionCtx) })
			log.Info().
				Dur("interval", retentionPolicy.OrphanFiles.Interval).
				Dur("grace", retentionPolicy.OrphanFiles.Grace).
				Int("max_files_per_run", retentionPolicy.OrphanFiles.MaxFilesPerRun).
				Int("batch_size", retentionPolicy.OrphanFiles.BatchSize).
				Msg("Orphan media scan enabled")
		}
	}

	// memory/durable 模式启动重投调度：进程重启后把 pending 命令重新投给已上线会话。
	// off 模式不写 outbox，也不启动重投。
	cancelOutbox := func() {}
	var outboxDone <-chan struct{}
	if outbox.Enabled() {
		// 由内置管控负责把应答/失败事件落回 outbox，并在失败时决定重试还是终结。
		serv.SetReliableResultHandler(outbox.HandleResult)
		outboxCtx, cancel := context.WithCancel(context.Background())
		cancelOutbox = cancel
		outboxDone = outbox.Done()
		routines.GoSafe(func() { outbox.Run(outboxCtx) })
	}
	// 上行 HTTP 出口在后台消费 best-effort 队列；confirm-before-ack 由会话 goroutine 同步调用。
	cancelUplinkSink := func() {}
	var uplinkSinkDone <-chan struct{}
	if uplinkSink != nil {
		sinkCtx, cancel := context.WithCancel(context.Background())
		cancelUplinkSink = cancel
		done := make(chan struct{})
		uplinkSinkDone = done
		routines.GoSafe(func() {
			defer close(done)
			uplinkSink.Run(sinkCtx)
		})
	}

	if alarmOptions.AutoUpload {
		hooks = alarmserver.NewUploadTrigger(hooks, alarmserver.UploadTriggerOptions{
			IP:      alarmOptions.UploadIP,
			TCPPort: alarmOptions.Port,
			UDPPort: alarmOptions.UDPPort,
		}, func(phone string, msg *model.Msg9208) error {
			ctx := context.Background()
			device, err := authProvider.Device(ctx, phone)
			if err != nil {
				return fmt.Errorf("device is offline: %w", err)
			}
			session, err := deps.Runtime.Conn.GetByPhone(ctx, device.Phone)
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
	var udpServ *server.UDPServer
	if cfg.Server.Port.UDPPort != "" {
		udpServ = server.NewUDPServer()
		udpServ.SetDeps(deps)
		udpServ.SetHooks(hooks)
		udpServ.SetMetrics(metrics)
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
			Retention:     alarmOptions.Retention,
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

	var mediaServ *media.Server
	if mediaEnabled {
		mediaServ = media.NewServer(media.Config{
			Dir:           mediaOptions.Dir,
			MaxConcurrent: mediaOptions.MaxConcurrent,
			MaxTCPConns:   mediaOptions.MaxTCPConns,
			Retention:     mediaOptions.Retention,
		})
		if mediaOptions.Port != 0 {
			mediaUDPAddr := fmt.Sprintf(":%d", mediaOptions.Port)
			if err := mediaServ.Listen(mediaUDPAddr); err != nil {
				log.Error().Err(err).Str("addr", mediaUDPAddr).Msg("Fail to listen media udp addr")
				os.Exit(1)
			}
		}
		if mediaOptions.TCPPort != 0 {
			mediaTCPAddr := fmt.Sprintf(":%d", mediaOptions.TCPPort)
			if err := mediaServ.ListenTCP(mediaTCPAddr); err != nil {
				log.Error().Err(err).Str("addr", mediaTCPAddr).Msg("Fail to listen media tcp addr")
				os.Exit(1)
			}
		}
		routines.GoSafe(func() { mediaServ.Start() })
	}

	// 就绪检查：存储连通、outbox 过期堆积、媒体/附件目录可写。
	// 未启用的组件不参与判定，避免把“功能关闭”误报成“服务异常”。
	checks := []observability.Check{{
		Name: "storage",
		Fn: func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			return stores.Check(ctx)
		},
	}}
	if outbox.Enabled() {
		checks = append(checks, observability.Check{
			Name: "outbox_backlog",
			Fn: func(ctx context.Context) error {
				return outbox.CheckBacklog(ctx, observability.DefaultExpiredOutboxThreshold)
			},
		})
	}
	if mediaEnabled {
		checks = append(checks, observability.Check{
			Name: "media_disk",
			Fn:   func(context.Context) error { return observability.CheckWritableDir(mediaOptions.Dir) },
		})
	}
	if alarmEnabled {
		checks = append(checks, observability.Check{
			Name: "attachment_disk",
			Fn:   func(context.Context) error { return observability.CheckWritableDir(alarmOptions.Dir) },
		})
	}
	checks = append(checks, observability.Check{
		Name: "media_upload_disk",
		Fn:   func(context.Context) error { return observability.CheckWritableDir(mediaUploadOptions.Dir) },
	})
	probes := &observability.Provider{
		Metrics: metrics,
		Checks:  checks,
		Collect: func(ctx context.Context) observability.Snapshot {
			snapshot := observability.Snapshot{ConnectedSessions: deps.Runtime.Conn.Count(ctx)}
			for _, binding := range deps.Runtime.Conn.ListBindings(ctx) {
				snapshot.AckPending += binding.Session.PendingCount()
			}
			if outbox.Enabled() {
				pending, expired, err := outbox.Stats(ctx)
				if err != nil {
					log.Warn().Err(err).Msg("Fail to collect outbox metrics")
				} else {
					snapshot.OutboxPending = pending
					snapshot.OutboxExpired = expired
				}
			}
			if mediaServ != nil {
				snapshot.MediaStreams = control.NewLocalMedia(mediaServ, mediaUploadStore).Streams()
			}
			if alarmServ != nil {
				reader := control.NewLocalAttachment(alarmServ.Assembler(), alarmOptions.Dir)
				snapshot.AttachmentsActive, snapshot.AttachmentsCompleted = reader.Stats()
			}
			// 媒体文件存量：数量与总字节数，用于发现磁盘无声膨胀。
			count, bytes, err := stores.MediaFiles.Stats(ctx)
			if err != nil {
				log.Warn().Err(err).Msg("Fail to collect media file metrics")
			} else {
				snapshot.MediaFiles = count
				snapshot.MediaFileBytes = bytes
			}
			if uplinkSink != nil {
				stats := uplinkSink.Stats()
				snapshot.Uplink = observability.UplinkSnapshot{
					Delivered:  stats.Delivered,
					Failed:     stats.Failed,
					Timeouts:   stats.Timeouts,
					Dropped:    stats.Dropped,
					Rejected:   stats.Rejected,
					InFlight:   stats.InFlight,
					QueueDepth: stats.QueueDepth,
				}
			}
			return snapshot
		},
	}

	var adminServ *adminapi.Server
	if adminEnabled {
		adminServ = adminapi.NewServer(adminOptions, adminapi.Dependencies{
			Conn:      deps.Runtime.Conn,
			Heartbeat: deps.Runtime.Heartbeat,
			Devices:   stores.Devices,
			Locations: stores.Locations,
			Alarms:    stores.Alarms,
			Probes:    probes,
		})
		if err := adminServ.Listen(adminOptions.Addr); err != nil {
			log.Error().Err(err).Str("addr", adminOptions.Addr).Msg("Fail to listen admin API addr")
			os.Exit(1)
		}
		routines.GoSafe(func() { _ = adminServ.Start() })
	}

	apiCtx, cancelAPI := context.WithCancel(context.Background())
	apiDone := make(chan struct{})
	routines.GoSafe(func() {
		defer close(apiDone)
		api.Run(apiCtx, serv, cfg, apiOptions, alarmServ, outbox, mediaUploadStore)
	})

	// 运行时状态只存在于进程内，进程退出即自然归零；数据库无需、也不应被批量重置。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Info().Str("signal", sig.String()).Msg("Shutting down")

	// 关机顺序：先停业务入口，再停终端接入，最后排空投递与落盘。
	// 1. 业务 API 不再受理新的下发请求，并等待在途 HTTP 请求完成。
	cancelAPI()
	select {
	case <-apiDone:
	case <-time.After(10 * time.Second):
		log.Warn().Msg("Timed out waiting for business API to shut down")
	}

	// 2. 停止接入新终端并关闭存量会话，后续不再产生新的上行写入。
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	if err := serv.Shutdown(shutdownCtx); err != nil {
		log.Warn().Err(err).Msg("Fail to shutdown TCP server")
	}
	cancelShutdown()
	if udpServ != nil {
		udpServ.Stop()
	}

	// 3. 取消重投循环并等待当前 tick 收尾；未投递的命令留在 outbox 等下次启动。
	cancelOutbox()
	if outboxDone != nil {
		select {
		case <-outboxDone:
		case <-time.After(5 * time.Second):
			log.Warn().Msg("Timed out waiting for command outbox to drain")
		}
	}

	// 4. 停止上行出口，给在途投递一个收尾窗口；未完成事件随进程退出丢弃。
	cancelUplinkSink()
	if uplinkSinkDone != nil {
		select {
		case <-uplinkSinkDone:
		case <-time.After(5 * time.Second):
			log.Warn().Msg("Timed out waiting for uplink sink to drain")
		}
	}

	// 5. 停止清理与扫描任务，等待当前批次收尾，避免与存储关闭竞争。
	cancelRetention()
	if len(retentionDones) > 0 {
		timer := time.NewTimer(5 * time.Second)
	waitRetention:
		for _, done := range retentionDones {
			select {
			case <-done:
			case <-timer.C:
				log.Warn().Msg("Timed out waiting for retention cleanup to stop")
				break waitRetention
			}
		}
		timer.Stop()
	}

	// 6. 关闭媒体与附件写入，确保文件句柄与已收数据落盘。
	if alarmServ != nil {
		alarmServ.Stop()
	}
	if mediaServ != nil {
		mediaServ.Stop()
	}

	// 7. 管理面最后停止，尽量延长探针与指标的可用窗口。
	if adminServ != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := adminServ.Stop(ctx); err != nil {
			log.Warn().Err(err).Msg("Fail to stop admin API server")
		}
		cancel()
	}
	if err := storage.Close(); err != nil {
		log.Warn().Err(err).Msg("Fail to close storage backend")
	}
}
