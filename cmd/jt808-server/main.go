package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/adminapi"
	"github.com/fakeyanss/jt808-server-go/internal/alarmserver"
	"github.com/fakeyanss/jt808-server-go/internal/api"
	"github.com/fakeyanss/jt808-server-go/internal/blob"
	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/control"
	"github.com/fakeyanss/jt808-server-go/internal/downlinkhttp"
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

	// 运行形态必须在 storage.Init 之前 fail-fast：配置矛盾时不要先连数据库。
	clusterOptions, clusterErr := cfg.ParseServerMode()
	if clusterErr != nil {
		log.Error().Err(clusterErr).Msg("Invalid server mode / cluster configuration")
		os.Exit(1)
	}
	standalone := clusterOptions.IsStandalone()
	hasGateway := standalone || clusterOptions.Roles.Has(config.RoleGateway)
	hasControl := standalone || clusterOptions.Roles.Has(config.RoleControl)
	hasMedia := standalone || clusterOptions.Roles.Has(config.RoleMedia)
	hasAttachment := standalone || clusterOptions.Roles.Has(config.RoleAttachment)
	log.Info().
		Str("mode", clusterOptions.Mode).
		Strs("roles", clusterOptions.Roles.List()).
		Msg("Server mode resolved")

	// gateway/control 需要共享库；纯 media/attachment 进程不连业务库。
	var stores *storage.Stores
	if hasGateway || hasControl {
		initialized, err := storage.Init(storage.FromConfig(cfg))
		if err != nil {
			log.Error().Err(err).Msg("Fail to initialize storage backend")
			os.Exit(1)
		}
		stores = initialized
		log.Info().Str("backend", stores.Backend).Msg("Storage backend initialized")
	}

	// mode 未显式配置时按存储后端归一化：memory 后端默认 memory，SQL 后端默认 durable。
	var commandRepo repository.CommandRepository
	outboxMode := config.CommandOutboxModeOff
	if hasControl {
		outboxMode = config.ResolveCommandOutboxMode(outboxOptions.Mode, stores.Backend)
		switch outboxMode {
		case config.CommandOutboxModeOff:
		case config.CommandOutboxModeMemory:
			commandRepo = repository.NewCommandRepository()
		case config.CommandOutboxModeDurable:
			commandRepo = stores.Commands
		}
		log.Info().Str("mode", outboxMode).Msg("Command outbox mode resolved")
	}

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

	// 指标在出口之前创建：409 丢弃、超限丢弃等事件需要在网关侧计数。
	metrics := observability.NewMetrics()

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
			OnUnknownDeviceDropped: func(phone string) {
				metrics.IncUplinkUnknownDropped()
			},
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
	// 纯 media/attachment 进程不处理 0x0801，也不需要该目录。
	var mediaUploadStore *mediaupload.Store
	if hasGateway || hasControl {
		mediaUploadOptions, mediaUploadErr := cfg.ParseMediaUpload()
		if mediaUploadErr != nil {
			log.Error().Err(mediaUploadErr).Msg("Invalid mediaUpload configuration")
			os.Exit(1)
		}
		mediaUploadStore, mediaUploadErr = mediaupload.NewStore(mediaUploadOptions.Dir)
		if mediaUploadErr != nil {
			log.Error().Err(mediaUploadErr).Msg("Fail to initialize media upload store")
			os.Exit(1)
		}
		deps.MediaUpload = mediaUploadStore
		log.Info().Str("dir", mediaUploadStore.Root()).Msg("Media upload store initialized")
	}

	// 鉴权提供者是网关只读查询设备档案的契约：gateway+control 同进程直接读本地库，
	// gateway-only 通过管控内部入口查询（与 uplink 出口同源）。
	var authProvider contracts.AuthProvider
	switch {
	case hasControl:
		authProvider = control.NewLocalAuthProvider(stores.Devices)
	case hasGateway && uplinkSinkOptions.Mode == config.UplinkSinkHTTP:
		parsed, parseErr := url.Parse(uplinkSinkOptions.HTTP.URL)
		if parseErr != nil || parsed.Scheme == "" || parsed.Host == "" {
			log.Error().Str("url", uplinkSinkOptions.HTTP.URL).Msg("Invalid uplink.http.url for device lookup")
			os.Exit(1)
		}
		provider, providerErr := control.NewHTTPAuthProvider(
			parsed.Scheme+"://"+parsed.Host, uplinkSinkOptions.HTTP.Token,
			uplinkSinkOptions.HTTP.Timeout, 5*time.Minute)
		if providerErr != nil {
			log.Error().Err(providerErr).Msg("Fail to create HTTP auth provider")
			os.Exit(1)
		}
		authProvider = provider
	}
	deps.AuthProvider = authProvider
	if uplinkSink != nil {
		// 网关出口才需要本地注册缓存与消息体上限；standalone/local 不走事件出口。
		deps.RegisterCache = protocol.NewRegisterCache(5 * time.Minute)
		deps.UplinkMaxBodyBytes = clusterOptions.Uplink.MaxBodyBytes
		deps.OnOversized = func(msgID uint16, phone string, size int) {
			metrics.IncUplinkOversized()
		}
	}

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

	var serv *server.TCPServer
	if hasGateway {
		serv = server.NewTCPServer()
		serv.SetDeps(deps)
		serv.SetReliability(time.Duration(cfg.Server.AckTimeoutMS)*time.Millisecond, cfg.Server.MaxRetransmissions)
		serv.SetMetrics(metrics)
	}

	// durable outbox 归内置管控模块所有：幂等、租约、退避、过期与重投都在这里，
	// 协议层只上报可靠投递结果。control-only 下 Conn 为 nil，离线判断交给网关回执。
	var outbox *control.Outbox
	var controlDownlink contracts.DownlinkSender
	if hasControl {
		var downlinkSender contracts.DownlinkSender
		if hasGateway {
			sender, senderErr := control.NewLocalSender(control.LocalSenderDependencies{
				Conn: deps.Runtime.Conn,
				Send: func(sessionID string, msg model.JT808Msg, serial uint16) error {
					return serv.Send(sessionID, msg, server.WithSerial(serial))
				},
			})
			if senderErr != nil {
				log.Error().Err(senderErr).Msg("Fail to create local downlink sender")
				os.Exit(1)
			}
			downlinkSender = sender
		} else {
			client, clientErr := downlinkhttp.NewClient(downlinkhttp.ClientOptions{
				BaseURL: clusterOptions.Gateway.URL,
				Token:   clusterOptions.Gateway.Token,
				Timeout: clusterOptions.Downlink.Timeout,
			})
			if clientErr != nil {
				log.Error().Err(clientErr).Msg("Fail to create HTTP downlink client")
				os.Exit(1)
			}
			downlinkSender = client
			log.Info().Str("gateway_url", clusterOptions.Gateway.URL).Msg("HTTP downlink client enabled")
		}
		built, buildErr := control.NewOutbox(control.OutboxDependencies{
			Commands: commandRepo,
			Devices:  stores.Devices,
			Metrics:  metrics,
			Send:     downlinkSender,
		}, control.OutboxOptions{
			Enabled:                outboxMode != config.CommandOutboxModeOff,
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
		if buildErr != nil {
			log.Error().Err(buildErr).Msg("Fail to create command outbox")
			os.Exit(1)
		}
		outbox = built
		controlDownlink = downlinkSender
	}

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
	if hasControl && retentionPolicy.Cleanup.Enabled {
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
	if hasControl && retentionPolicy.OrphanFiles.Enabled {
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

	// 网关侧下行 HTTP 入口只在 cluster 形态创建；standalone 不新增任何监听。
	var downlinkServer *downlinkhttp.Server
	if hasGateway && !standalone {
		srv, srvErr := downlinkhttp.NewServer(downlinkhttp.Dependencies{
			Token:       clusterOptions.Gateway.Token,
			Timeout:     clusterOptions.Downlink.Timeout,
			IDTTL:       clusterOptions.Downlink.IDTTL,
			MaxInFlight: clusterOptions.Downlink.MaxInFlight,
			Conn:        deps.Runtime.Conn,
			Send: func(ctx context.Context, sessionID string, msg model.JT808Msg, serial uint16) error {
				return serv.Send(sessionID, msg, server.WithSerial(serial))
			},
		})
		if srvErr != nil {
			log.Error().Err(srvErr).Msg("Fail to create downlink HTTP server")
			os.Exit(1)
		}
		if err := srv.Listen(clusterOptions.Gateway.Addr); err != nil {
			log.Error().Err(err).Str("addr", clusterOptions.Gateway.Addr).Msg("Fail to listen downlink HTTP addr")
			os.Exit(1)
		}
		downlinkServer = srv
		// 查询应答（0x0104/0x0107/0x0201/0x0608）在网关侧回填等待表，
		// 由控制面通过下行契约取回消息体。
		deps.QueryResponse = srv.HandleQueryResponse
		if serv != nil {
			serv.SetDeps(deps)
		}
		log.Info().Str("addr", srv.Addr()).Msg("Downlink HTTP server listening")
	}

	// 管控侧上行 HTTP 入口：E2 把业务处理内置到控制面，网关只投事件。
	// 部署建议：E2 默认单副本（同 phone 串行 mailbox 是进程内实现），但多副本在
	// 字段所有权隔离 + 409 有界重投 + eventId 去重下是安全的；程序无法检测副本数。
	var uplinkServer *uplink.Server
	var controlBlob contracts.BlobStore
	var businessExecutor *control.BusinessExecutor
	cancelSweep := func() {}
	if hasControl && !standalone {
		log.Info().Msg("control uplink enabled: phone-ordered mailbox is process-local; single replica recommended, multiple replicas supported with per-field ownership")
		if clusterOptions.Blob.Backend == config.BlobBackendSharedVolume {
			shared, blobErr := blob.NewSharedVolume(clusterOptions.Blob.Root, true)
			if blobErr != nil {
				log.Error().Err(blobErr).Str("root", clusterOptions.Blob.Root).Msg("Fail to initialize shared volume blob store")
				os.Exit(1)
			}
			controlBlob = shared
			log.Info().Str("root", clusterOptions.Blob.Root).Msg("Shared volume blob store initialized")
		}
		srv, srvErr := uplink.NewServer(uplink.ServerOptions{
			Token:               clusterOptions.Control.Token,
			DedupTTL:            clusterOptions.Uplink.DedupTTL,
			DedupMaxEntries:     clusterOptions.Uplink.DedupMaxEntries,
			MaxRequestBytes:     clusterOptions.Uplink.MaxRequestBytes,
			MaxDecodedBodyBytes: clusterOptions.Uplink.MaxDecodedBodyBytes,
		})
		if srvErr != nil {
			log.Error().Err(srvErr).Msg("Fail to create uplink HTTP server")
			os.Exit(1)
		}
		if outbox != nil {
			srv.SetTerminalReplyHandler(outbox)
		}
		executor := control.NewBusinessExecutor(control.BusinessOptions{
			ProcessorDeps: deps,
			Devices:       stores.Devices,
			Files:         stores.MediaFiles,
			Blob:          controlBlob,
			MediaUpload:   mediaUploadStore,
			OnUnknownDevice: func(phone string) {
				log.Error().Str("phone", phone).Msg("Uplink event rejected: device is not registered")
			},
			OnMediaTypeRejected: func(phone string, mediaType uint8) {
				metrics.IncMediaTypeRejected()
			},
			OnBlobCommitFailed: func(phone string, mediaID uint32) {
				metrics.IncBlobCommitFailed()
			},
		})
		businessExecutor = executor
		srv.RegisterDefaultConsumer(executor)
		// 0x0200/0x0704 默认 at_least_once；0x0801 强制同步确认。
		srv.RegisterConsumer(0x0200, executor)
		srv.RegisterConsumer(0x0704, executor)
		srv.RegisterConsumer(0x0801, executor)
		srv.SetDeviceLookup(func(ctx context.Context, phone string) (*model.Device, error) {
			return stores.Devices.GetByPhone(ctx, phone)
		})
		// 缺失 confirm-before-ack consumer 时启动失败，并一次性列出缺失项。
		if err := srv.ValidateCoverage(uplinkPolicy); err != nil {
			log.Error().Err(err).Msg("Uplink consumer coverage validation failed")
			os.Exit(1)
		}
		if err := srv.Listen(clusterOptions.Control.Addr); err != nil {
			log.Error().Err(err).Str("addr", clusterOptions.Control.Addr).Msg("Fail to listen uplink HTTP addr")
			os.Exit(1)
		}
		uplinkServer = srv
		log.Info().Str("addr", srv.Addr()).Msg("Uplink HTTP server listening")
	}

	// 共享卷 pending 对象巡检：写文件与写库之间中断时，无元数据引用的对象会被清理。
	if controlBlob != nil && businessExecutor != nil {
		sweepCtx, cancel := context.WithCancel(context.Background())
		cancelSweep = cancel
		routines.GoSafe(func() {
			ticker := time.NewTicker(10 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-sweepCtx.Done():
					return
				case <-ticker.C:
					businessExecutor.SweepPending(sweepCtx, 30*time.Minute)
				}
			}
		})
	}

	// memory/durable 模式启动重投调度：进程重启后把 pending 命令重新投给已上线会话。
	// off 模式不写 outbox，也不启动重投。
	cancelOutbox := func() {}
	var outboxDone <-chan struct{}
	if outbox != nil && outbox.Enabled() {
		// 同一进程内网关的应答/失败事件同时喂给下行在途表与 outbox。
		if serv != nil {
			serv.SetReliableResultHandler(func(result model.ReliableResult) {
				if downlinkServer != nil {
					downlinkServer.HandleReliableResult(result)
				}
				outbox.HandleResult(result)
			})
		}
		outboxCtx, cancel := context.WithCancel(context.Background())
		cancelOutbox = cancel
		outboxDone = outbox.Done()
		routines.GoSafe(func() { outbox.Run(outboxCtx) })
	} else if serv != nil && downlinkServer != nil {
		serv.SetReliableResultHandler(downlinkServer.HandleReliableResult)
	}

	// 两个内部 HTTP 入口在后台接收请求。
	if downlinkServer != nil {
		srv := downlinkServer
		routines.GoSafe(func() { _ = srv.Start() })
	}
	if uplinkServer != nil {
		srv := uplinkServer
		routines.GoSafe(func() { _ = srv.Start() })
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

	if hasGateway && hasControl && alarmOptions.AutoUpload {
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
	if serv != nil {
		serv.SetHooks(hooks)
	}
	if hasGateway {
		addr := ":" + cfg.Server.Port.TCPPort
		if err := serv.Listen(addr); err != nil {
			log.Error().Err(err).Str("addr", addr).Msg("Fail to listen tcp addr")
			os.Exit(1)
		}
		routines.GoSafe(func() { serv.Start() })
	}
	var udpServ *server.UDPServer
	if hasGateway && cfg.Server.Port.UDPPort != "" {
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
	if hasAttachment && alarmEnabled {
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
	if hasMedia && mediaEnabled {
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
	checks := []observability.Check{}
	if stores != nil {
		checks = append(checks, observability.Check{
			Name: "storage",
			Fn: func(ctx context.Context) error {
				ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				return stores.Check(ctx)
			},
		})
	}
	if outbox != nil && outbox.Enabled() {
		checks = append(checks, observability.Check{
			Name: "outbox_backlog",
			Fn: func(ctx context.Context) error {
				return outbox.CheckBacklog(ctx, observability.DefaultExpiredOutboxThreshold)
			},
		})
	}
	if hasMedia && mediaEnabled {
		checks = append(checks, observability.Check{
			Name: "media_disk",
			Fn:   func(context.Context) error { return observability.CheckWritableDir(mediaOptions.Dir) },
		})
	}
	if hasAttachment && alarmEnabled {
		checks = append(checks, observability.Check{
			Name: "attachment_disk",
			Fn:   func(context.Context) error { return observability.CheckWritableDir(alarmOptions.Dir) },
		})
	}
	if mediaUploadStore != nil {
		checks = append(checks, observability.Check{
			Name: "media_upload_disk",
			Fn:   func(context.Context) error { return observability.CheckWritableDir(mediaUploadStore.Root()) },
		})
	}
	probes := &observability.Provider{
		Metrics: metrics,
		Checks:  checks,
		Collect: func(ctx context.Context) observability.Snapshot {
			snapshot := observability.Snapshot{ConnectedSessions: deps.Runtime.Conn.Count(ctx)}
			for _, binding := range deps.Runtime.Conn.ListBindings(ctx) {
				snapshot.AckPending += binding.Session.PendingCount()
			}
			if outbox != nil && outbox.Enabled() {
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
			if stores != nil {
				count, bytes, err := stores.MediaFiles.Stats(ctx)
				if err != nil {
					log.Warn().Err(err).Msg("Fail to collect media file metrics")
				} else {
					snapshot.MediaFiles = count
					snapshot.MediaFileBytes = bytes
				}
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
	if hasControl && adminEnabled {
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
	if hasControl {
		// control-only 没有本地会话，查询 broker 通过下行契约交给网关等待终端应答；
		// gateway+control 同进程继续使用进程内 broker。
		var queryer api.Queryer
		if !hasGateway && controlDownlink != nil {
			queryer = &control.DownlinkQueryer{Sender: controlDownlink}
		}
		routines.GoSafe(func() {
			defer close(apiDone)
			api.Run(apiCtx, serv, cfg, apiOptions, alarmServ, outbox, controlDownlink, mediaUploadStore, queryer)
		})
	} else {
		close(apiDone)
	}

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

	// 2. 停止两个内部 HTTP 入口，避免关机过程中继续受理下发/上行。
	if downlinkServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := downlinkServer.Stop(ctx); err != nil {
			log.Warn().Err(err).Msg("Fail to stop downlink HTTP server")
		}
		cancel()
	}
	if uplinkServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := uplinkServer.Stop(ctx); err != nil {
			log.Warn().Err(err).Msg("Fail to stop uplink HTTP server")
		}
		cancel()
	}

	// 3. 停止接入新终端并关闭存量会话，后续不再产生新的上行写入。
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	if serv != nil {
		if err := serv.Shutdown(shutdownCtx); err != nil {
			log.Warn().Err(err).Msg("Fail to shutdown TCP server")
		}
	}
	cancelShutdown()
	if udpServ != nil {
		udpServ.Stop()
	}

	// 3. 取消重投循环并等待当前 tick 收尾；未投递的命令留在 outbox 等下次启动。
	cancelSweep()
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
	if stores != nil {
		if err := storage.Close(); err != nil {
			log.Warn().Err(err).Msg("Fail to close storage backend")
		}
	}
}
