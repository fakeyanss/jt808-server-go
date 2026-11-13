// Package app 是服务端的共享装配层。
//
// 每个 cmd 只声明自己固定承担的角色；具体启动哪些组件、按什么顺序退出，
// 全部由本包按角色推导，避免多个二进制各复制一份 main。
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/adminapi"
	"github.com/fakeyanss/jt808-server-go/internal/alarmserver"
	"github.com/fakeyanss/jt808-server-go/internal/api"
	"github.com/fakeyanss/jt808-server-go/internal/attachmentserver"
	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/control"
	"github.com/fakeyanss/jt808-server-go/internal/downlinkhttp"
	"github.com/fakeyanss/jt808-server-go/internal/media"
	"github.com/fakeyanss/jt808-server-go/internal/media/playback"
	"github.com/fakeyanss/jt808-server-go/internal/mediaupload"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/repository/database"
	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
	"github.com/fakeyanss/jt808-server-go/internal/retention"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
	"github.com/fakeyanss/jt808-server-go/internal/server"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/fakeyanss/jt808-server-go/internal/uplink"
	"github.com/fakeyanss/jt808-server-go/pkg/logger"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

// Spec 是一个二进制的固定角色声明。
//
// Mode 为空表示通用入口（jt808-server）：完全按配置文件运行，不做角色校验。
type Spec struct {
	// Binary 是二进制名，用于配置不匹配时的错误提示。
	Binary string
	// Mode 是固定的 server.mode；standalone 或 cluster。
	Mode string
	// RequiredRoles 是必须出现的角色。
	RequiredRoles []string
	// OptionalRoles 是允许额外出现的角色；固定角色二进制默认留空。
	OptionalRoles []string
}

// Main 是五个角色二进制与通用入口共用的启动入口。
// errAttachmentBlobRequired 表示附件角色缺少共享对象存储。
//
// 前置依赖：config.ValidateServerMode 对 media/attachment 角色强制
// cluster.storage.backend ∈ {sharedVolume, objectStore}（见 config.go 的 needsBlob 分支）；
// 这两种后端都会在 Main 前半段给 controlBlob 赋值。因此正常启动走不到这里，
// 本守卫只用于防御将来放宽校验、或直接构造依赖时把 nil 传进 attachmentserver.New。
var errAttachmentBlobRequired = errors.New("attachment role requires cluster.storage.backend=objectStore (accepted values: local|sharedVolume|objectStore; attachment role only supports objectStore) with a shared object store")

// checkAttachmentBlob 校验附件角色是否具备共享对象存储依赖。
func checkAttachmentBlob(blob contracts.BlobStore) error {
	if blob == nil {
		return errAttachmentBlobRequired
	}
	return nil
}

func Main(spec Spec) {
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

	// 运行形态必须在 database.Init 之前 fail-fast：配置矛盾时不要先连数据库。
	clusterOptions, clusterErr := cfg.ParseServerMode()
	if clusterErr != nil {
		log.Error().Err(clusterErr).Msg("Invalid server mode / cluster configuration")
		os.Exit(1)
	}
	if err := spec.validate(clusterOptions); err != nil {
		log.Error().Err(err).Msg("Server role mismatch")
		os.Exit(1)
	}

	probeAddr, probeEnabled, probeErr := cfg.ParseProbes()
	if probeErr != nil {
		log.Error().Err(probeErr).Msg("Invalid probes configuration")
		os.Exit(1)
	}
	if spec.Mode != "" && !probeEnabled {
		log.Error().Str("binary", spec.Binary).
			Msg("probes.addr is required for a role-specific binary; set probes.addr (for example 0.0.0.0:9105)")
		os.Exit(1)
	}

	standalone := clusterOptions.IsStandalone()
	hasGateway := standalone || clusterOptions.Roles.Has(config.RoleGateway)
	hasControl := standalone || clusterOptions.Roles.Has(config.RoleControl)
	hasMedia := standalone || clusterOptions.Roles.Has(config.RoleMedia)
	hasAttachment := standalone || clusterOptions.Roles.Has(config.RoleAttachment)
	log.Info().
		Str("binary", spec.Binary).
		Str("mode", clusterOptions.Mode).
		Strs("roles", clusterOptions.Roles.List()).
		Msg("Server mode resolved")
	// cluster.allowEmbeddedStorage 是 dev-only 开关：仅用于本地联调，
	// 生产集群必须使用共享 MySQL；开启时启动告警且就绪探针返回 degraded。
	embeddedStorage := !standalone && clusterOptions.AllowEmbeddedStorage &&
		cfg.Database != nil && strings.EqualFold(cfg.Database.Type, "sqlite")
	if embeddedStorage {
		log.Warn().Msg("cluster.allowEmbeddedStorage is dev-only: cluster roles are sharing an embedded sqlite file; production must use MySQL")
	}

	// gateway/control 需要共享库；附件节点也要写共享库，控制面才能查询进度。
	var storeSet *database.Repositories
	if hasGateway || hasControl || hasAttachment {
		initialized, err := database.Init(database.FromConfig(cfg))
		if err != nil {
			log.Error().Err(err).Msg("Fail to initialize database backend")
			os.Exit(1)
		}
		storeSet = initialized
		log.Info().Str("backend", storeSet.Backend).Msg("Database backend initialized")
	}

	// mode 未显式配置时按存储后端归一化：memory 后端默认 memory，SQL 后端默认 durable。
	var commandRepo repository.CommandRepository
	outboxMode := config.CommandOutboxModeOff
	if hasControl {
		outboxMode = config.ResolveCommandOutboxMode(outboxOptions.Mode, storeSet.Backend)
		switch outboxMode {
		case config.CommandOutboxModeOff:
		case config.CommandOutboxModeMemory:
			commandRepo = memory.NewCommandRepository()
		case config.CommandOutboxModeDurable:
			commandRepo = storeSet.Commands
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

	deps := protocol.ProcessorDeps{Repos: storeSet, Runtime: runtime.Default(), Uplink: &uplinkPolicy}

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
		authProvider = control.NewLocalAuthProvider(storeSet.Devices)
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

	// control 独立进程时通过 HTTP 调度媒体节点；同进程有本地媒体时优先本地。
	var remoteMedia *control.RemoteMedia
	if hasControl && !standalone {
		nodes := make([]control.MediaNode, 0, len(clusterOptions.Media.Nodes))
		for _, node := range clusterOptions.Media.Nodes {
			nodes = append(nodes, control.MediaNode{Name: node.Name, URL: node.URL})
		}
		remoteMedia = control.NewRemoteMedia(control.RemoteMediaOptions{Nodes: nodes})
		log.Info().Int("nodes", len(nodes)).Msg("Remote media allocator configured")
	}

	// 控制面通过 HTTP 查询附件节点状态并选择节点；同一 (phone, alarm) 保持粘性。
	var remoteAttachment *control.RemoteAttachment
	if hasControl && !standalone {
		nodes := make([]control.AttachmentNode, 0, len(clusterOptions.Attachment.Nodes))
		for _, node := range clusterOptions.Attachment.Nodes {
			nodes = append(nodes, control.AttachmentNode{Name: node.Name, URL: node.URL})
		}
		remoteAttachment = control.NewRemoteAttachment(control.RemoteAttachmentOptions{Nodes: nodes})
		log.Info().Int("nodes", len(nodes)).Msg("Remote attachment allocator configured")
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
			Devices:  storeSet.Devices,
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
			OfflineRetry:           outboxOptions.OfflineRetry,
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
		if storeSet.Retention == nil {
			log.Warn().Str("backend", storeSet.Backend).
				Msg("Retention cleanup is enabled but the database backend does not support it; skipped")
		} else {
			cleaner := retention.NewCleaner(storeSet.Retention, mediaUploadStore, retention.Config{
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
		if storeSet.Retention == nil || mediaUploadStore == nil {
			log.Warn().Str("backend", storeSet.Backend).
				Msg("Orphan media scan is enabled but the database backend does not support it; skipped")
		} else {
			scanner := retention.NewOrphanScanner(storeSet.Retention, mediaUploadStore, retention.OrphanConfig{
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
	// 集群文件后端：控制面用它承接 0x0801，媒体角色用它上传 segmented 录像。
	// standalone 只用本地目录，不初始化远端后端。
	if !standalone && clusterOptions.Storage.Backend != "" &&
		(clusterOptions.Roles.Has(config.RoleControl) || clusterOptions.Roles.Has(config.RoleMedia) ||
			clusterOptions.Roles.Has(config.RoleAttachment)) {
		if clusterOptions.Storage.Backend == config.BlobBackendSharedVolume {
			shared, blobErr := storage.NewSharedVolume(clusterOptions.Storage.Root, true)
			if blobErr != nil {
				log.Error().Err(blobErr).Str("root", clusterOptions.Storage.Root).Msg("Fail to initialize shared volume blob store")
				os.Exit(1)
			}
			controlBlob = shared
			log.Info().Str("root", clusterOptions.Storage.Root).Msg("Shared volume blob store initialized")
		} else if clusterOptions.Storage.Backend == config.BlobBackendObjectStore {
			store := clusterOptions.Storage.ObjectStore
			objectStore, blobErr := storage.NewObjectStore(storage.ObjectStoreConfig{
				Endpoint:  store.Endpoint,
				Region:    store.Region,
				Bucket:    store.Bucket,
				AccessKey: store.AccessKey,
				SecretKey: store.SecretKey,
				PathStyle: store.PathStyle,
				Prefix:    store.Prefix,
				Retries:   store.Retries,
			})
			if blobErr != nil {
				log.Error().Err(blobErr).Str("endpoint", store.Endpoint).Msg("Fail to initialize object store blob backend")
				os.Exit(1)
			}
			controlBlob = objectStore
			log.Info().Str("endpoint", store.Endpoint).Str("bucket", store.Bucket).Str("prefix", store.Prefix).
				Msg("Object store blob backend initialized")
		}
	}
	if hasControl && !standalone {
		log.Info().Msg("control uplink enabled: phone-ordered mailbox is process-local; single replica recommended, multiple replicas supported with per-field ownership")
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
		// 控制面自己识别报警并下发 0x9208：附件节点地址来自 HTTP 节点状态，
		// 不再要求 gateway 与 control 同进程。
		controlHooks := hooks
		if alarmOptions.AutoUpload && hasControl && remoteAttachment != nil && controlDownlink != nil {
			controlHooks = control.NewAutoUploadTrigger(hooks, remoteAttachment, controlDownlink, 30*time.Minute)
			log.Info().Msg("Automatic 0x9208 attachment upload trigger enabled in control via remote attachment nodes")
		}
		executor := control.NewBusinessExecutor(control.BusinessOptions{
			ProcessorDeps: deps,
			Devices:       storeSet.Devices,
			Files:         storeSet.MediaFiles,
			Blob:          controlBlob,
			MediaUpload:   mediaUploadStore,
			Hooks:         controlHooks,
			OnUnknownDevice: func(phone string) {
				log.Error().Str("phone", phone).Msg("Uplink event rejected: device is not registered")
			},
			OnMediaTypeRejected: func(phone string, mediaType uint8) {
				metrics.IncMediaTypeRejected()
			},
			OnStorageCommitFailed: func(phone string, mediaID uint32) {
				metrics.IncStorageCommitFailed()
			},
		})
		businessExecutor = executor
		srv.RegisterDefaultConsumer(executor)
		// 0x0200/0x0704 默认 at_least_once；0x0801 强制同步确认。
		srv.RegisterConsumer(0x0200, executor)
		srv.RegisterConsumer(0x0704, executor)
		srv.RegisterConsumer(0x0801, executor)
		srv.SetDeviceLookup(func(ctx context.Context, phone string) (*model.Device, error) {
			return storeSet.Devices.GetByPhone(ctx, phone)
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

	switch {
	case alarmOptions.AutoUpload && standalone:
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
	case alarmOptions.AutoUpload && hasControl:
		// 已在 BusinessExecutor 上装配 AutoUploadTrigger，这里不重复安装。
	case alarmOptions.AutoUpload:
		// 没有 control 角色就没有报警识别与下发方；不能静默消失。
		metrics.IncAutoUploadSkipped()
		log.Warn().
			Str("binary", spec.Binary).
			Strs("roles", clusterOptions.Roles.List()).
			Msg("alarmServer.autoUpload is enabled but this binary has no control role; automatic 0x9208 trigger cannot run here")
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
	var attachmentServ *attachmentserver.Server
	if hasAttachment && alarmEnabled && standalone {
		// standalone 保持本地目录落盘，附件仍在同一进程内。
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
	} else if hasAttachment && alarmEnabled {
		// cluster 附件节点：本地只保留有界暂存，切片写入共享 objectStore，
		// 元数据落共享库供控制面查询。
		if err := checkAttachmentBlob(controlBlob); err != nil {
			log.Error().Msg(err.Error())
			os.Exit(1)
		}
		nodeID := clusterOptions.Attachment.NodeID
		if nodeID == "" {
			nodeID = fmt.Sprintf("%s:%d", alarmOptions.UploadIP, alarmOptions.Port)
		}
		built, err := attachmentserver.New(attachmentserver.Config{
			Dir:             alarmOptions.Dir,
			TCPPort:         alarmOptions.Port,
			UDPPort:         alarmOptions.UDPPort,
			AdvertiseIP:     alarmOptions.UploadIP,
			NodeID:          nodeID,
			MaxFileSize:     alarmOptions.MaxFileSize,
			MaxConcurrent:   alarmOptions.MaxConcurrent,
			SessionTimeout:  alarmserver.DefaultSessionTimeout,
			CompletedTTL:    alarmOptions.CompletedTTL,
			Retention:       alarmOptions.Retention,
			Files:           storeSet.AttachmentFiles,
			Blob:            controlBlob,
			StagingDir:      filepath.Join(alarmOptions.Dir, "staging"),
			SegmentSize:     alarmOptions.SegmentSizeBytes,
			MaxStagingBytes: alarmOptions.StagingMaxBytes,
			HTTPAddr:        fmt.Sprintf(":%d", alarmOptions.HTTPPort),
		})
		if err != nil {
			log.Error().Err(err).Msg("Fail to create attachment server")
			os.Exit(1)
		}
		if alarmOptions.Port != 0 {
			if err := built.Listen(fmt.Sprintf(":%d", alarmOptions.Port)); err != nil {
				log.Error().Err(err).Msg("Fail to listen attachment tcp addr")
				os.Exit(1)
			}
		}
		if alarmOptions.UDPPort != 0 {
			if err := built.ListenUDP(fmt.Sprintf(":%d", alarmOptions.UDPPort)); err != nil {
				log.Error().Err(err).Msg("Fail to listen attachment udp addr")
				os.Exit(1)
			}
		}
		if alarmOptions.HTTPPort != 0 {
			if err := built.ListenHTTP(fmt.Sprintf(":%d", alarmOptions.HTTPPort)); err != nil {
				log.Error().Err(err).Str("addr", built.HTTPAddr()).Msg("Fail to listen attachment internal http addr")
				os.Exit(1)
			}
			log.Info().Str("addr", built.HTTPAddr()).Msg("Attachment internal HTTP listening")
		}
		attachmentServ = built
		routines.GoSafe(func() { attachmentServ.Start() })
		log.Info().Str("node", nodeID).Str("staging", filepath.Join(alarmOptions.Dir, "staging")).
			Msg("Attachment node started in object storage mode")
		// 通用入口同进程托管 control 与 attachment 且没有显式节点清单时，
		// 用本进程的内部 HTTP 作为附件节点，避免手工/自动 0x9208 误报“无可用节点”。
		if hasControl && remoteAttachment != nil && len(remoteAttachment.Nodes()) == 0 && alarmOptions.HTTPPort != 0 {
			localURL := fmt.Sprintf("http://127.0.0.1:%d", alarmOptions.HTTPPort)
			remoteAttachment = control.NewRemoteAttachment(control.RemoteAttachmentOptions{
				Nodes: []control.AttachmentNode{{Name: nodeID, URL: localURL}},
			})
			log.Info().Str("url", localURL).Msg("Attachment allocator falls back to the in-process attachment node")
		}
	}

	var mediaServ *media.Server
	if hasMedia && mediaEnabled {
		// cluster 默认不录制（实时流只进内存帧环），standalone 保持旧行为；
		// 需要转存时显式配置 record.mode=segmented。
		recordMode := mediaOptions.RecordMode
		if !mediaOptions.RecordModeExplicit {
			if strings.EqualFold(cfg.Server.Mode, "cluster") {
				recordMode = string(media.RecordModeOff)
			} else {
				recordMode = string(media.RecordModeLegacy)
			}
		}
		if recordMode == string(media.RecordModeOff) {
			log.Info().Msg("Media recording is disabled (record.mode=off): live frames stay in the in-memory ring")
		}
		playbackOpts := mediaOptions.Playback
		advertiseIP := mediaOptions.AdvertiseIP
		if advertiseIP == "" {
			advertiseIP = "127.0.0.1"
		}
		playbackConfig := playback.Config{
			Output: playback.OutputMode(playbackOpts.Output),
			FLV: playback.FLVHTTPOptions{
				BaseURL: fmt.Sprintf("http://%s:%d", advertiseIP, mediaOptions.HTTPPort),
			},
			RTMP: playback.RTMPConfig{
				URL:           playbackOpts.RTMPURL,
				StreamPrefix:  playbackOpts.StreamPrefix,
				PlayURLPrefix: playbackOpts.PlayURLPrefix,
				DialTimeout:   playbackOpts.DialTimeout,
				WriteTimeout:  playbackOpts.WriteTimeout,
				QueueFrames:   playbackOpts.QueueFrames,
			},
		}
		if playbackConfig.Output != playback.OutputRTMP {
			log.Info().Str("output", string(playbackConfig.Output)).
				Str("flvBaseUrl", playbackConfig.FLV.BaseURL).Msg("Media playback output configured")
		}
		if playbackConfig.Output == playback.OutputRTMP || playbackConfig.Output == playback.OutputBoth {
			// 桥接模式下观众直连 SRS，平台无法替观众做 GOP 对齐；
			// 必须保持 SRS 的 gop_cache 开启（默认值），否则新观众会黑屏/卡住。
			log.Info().Str("rtmp", playbackOpts.RTMPURL).
				Msg("RTMP playback output enabled; keep SRS gop_cache ON (default) for new viewers")
		}
		mediaServ = media.NewServer(media.Config{
			Dir:             mediaOptions.Dir,
			MaxConcurrent:   mediaOptions.MaxConcurrent,
			MaxTCPConns:     mediaOptions.MaxTCPConns,
			Retention:       mediaOptions.Retention,
			RecordMode:      media.RecordMode(recordMode),
			SegmentDuration: mediaOptions.SegmentDuration,
			RingBytes:       mediaOptions.RingBytes,
			RingDuration:    mediaOptions.RingDuration,
			Blob:            controlBlob,
			AdvertiseIP:     advertiseIP,
			NodeID:          clusterOptions.Media.NodeID,
			Playback:        playbackConfig,
		})
		mediaAddr := fmt.Sprintf(":%d", mediaOptions.Port)
		if err := mediaServ.Listen(mediaAddr); err != nil {
			log.Error().Err(err).Str("addr", mediaAddr).Msg("Fail to listen media addr")
			os.Exit(1)
		}
		httpAddr := fmt.Sprintf(":%d", mediaOptions.HTTPPort)
		if err := mediaServ.ListenHTTP(httpAddr); err != nil {
			log.Error().Err(err).Str("addr", httpAddr).Msg("Fail to listen media http addr")
			os.Exit(1)
		}
		routines.GoSafe(func() { mediaServ.Start() })
	}

	// 播放能力：同进程有媒体节点时直接本地调用，否则使用跨进程的媒体调度器。
	var playbackLibrary contracts.PlaybackLibrary
	switch {
	case mediaServ != nil:
		playbackLibrary = mediaServ
		if hasControl {
			log.Info().Msg("Media playback uses the in-process media server")
		}
	case remoteMedia != nil:
		playbackLibrary = remoteMedia
		log.Info().Int("nodes", len(remoteMedia.Nodes())).Msg("Media playback uses remote media nodes")
	}

	// 就绪检查：存储连通、outbox 过期堆积、媒体/附件目录可写。
	// 未启用的组件不参与判定，避免把“功能关闭”误报成“服务异常”。
	checks := []observability.Check{}
	if storeSet != nil {
		checks = append(checks, observability.Check{
			Name: "database",
			Fn: func(ctx context.Context) error {
				ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				return storeSet.Check(ctx)
			},
		})
	}
	if embeddedStorage {
		checks = append(checks, observability.Check{
			Name: "embedded_database",
			Fn: func(context.Context) error {
				return observability.Degraded(
					"cluster is running on embedded sqlite: cluster.allowEmbeddedStorage is dev-only and forbidden in production")
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
		attachmentDir := alarmOptions.Dir
		if !standalone {
			attachmentDir = filepath.Join(alarmOptions.Dir, "staging")
		}
		checks = append(checks, observability.Check{
			Name: "attachment_disk",
			Fn:   func(context.Context) error { return observability.CheckWritableDir(attachmentDir) },
		})
		if !standalone {
			checks = append(checks, observability.Check{
				Name: "attachment_object_store",
				Fn: func(context.Context) error {
					if controlBlob == nil || storeSet == nil || storeSet.AttachmentFiles == nil {
						return fmt.Errorf("attachment object store / shared attachment repository is not configured")
					}
					return nil
				},
			})
		}
	}
	if mediaUploadStore != nil {
		checks = append(checks, observability.Check{
			Name: "media_upload_disk",
			Fn:   func(context.Context) error { return observability.CheckWritableDir(mediaUploadStore.Root()) },
		})
	}
	// gateway-only：控制的地址必须可解析；否则上行事件全部无处投递。
	if !hasControl && hasGateway && uplinkSinkOptions.Mode == config.UplinkSinkHTTP {
		rawURL := uplinkSinkOptions.HTTP.URL
		checks = append(checks, observability.Check{
			Name: "control_endpoint",
			Fn: func(context.Context) error {
				return checkResolvableURL("uplink.http.url", rawURL)
			},
		})
	}
	// control：没有本地媒体时，必须至少有一个可达且有额度的远程媒体节点。
	if hasControl && mediaServ == nil && remoteMedia != nil {
		checks = append(checks, observability.Check{
			Name: "media_nodes",
			Fn: func(ctx context.Context) error {
				ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				return remoteMedia.Check(ctx)
			},
		})
	}
	// control：附件调度依赖节点清单与节点状态接口；空清单或无可用节点都视为未就绪。
	if hasControl && !standalone && remoteAttachment != nil {
		checks = append(checks, observability.Check{
			Name: "attachment_nodes",
			Fn: func(ctx context.Context) error {
				ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				return remoteAttachment.Check(ctx)
			},
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
			} else if remoteMedia != nil {
				snapshot.MediaStreams = remoteMedia.ActiveStreams(ctx)
			}
			if alarmServ != nil {
				reader := control.NewLocalAttachment(alarmServ.Assembler(), alarmOptions.Dir)
				snapshot.AttachmentsActive, snapshot.AttachmentsCompleted = reader.Stats()
			}
			// 媒体文件存量：数量与总字节数，用于发现磁盘无声膨胀。
			if storeSet != nil {
				count, bytes, err := storeSet.MediaFiles.Stats(ctx)
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
			Devices:   storeSet.Devices,
			Locations: storeSet.Locations,
			Alarms:    storeSet.Alarms,
			Probes:    probes,
		})
		if err := adminServ.Listen(adminOptions.Addr); err != nil {
			log.Error().Err(err).Str("addr", adminOptions.Addr).Msg("Fail to listen admin API addr")
			os.Exit(1)
		}
		routines.GoSafe(func() { _ = adminServ.Start() })
	}

	// 独立的存活/就绪探针：四个角色二进制都必须有，网关也不再依赖 admin 才能被探活。
	var probeServ *observability.ProbeServer
	if probeEnabled {
		started, listenErr := observability.ListenProbes(probeAddr, probes)
		if listenErr != nil {
			log.Error().Err(listenErr).Str("addr", probeAddr).Msg("Fail to listen probe addr")
			os.Exit(1)
		}
		probeServ = started
		routines.GoSafe(func() { _ = probeServ.Start() })
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
			var attachments repository.AttachmentFileRepository
			if !standalone && storeSet != nil {
				attachments = storeSet.AttachmentFiles
			}
			var allocator contracts.AttachmentAllocator
			if remoteAttachment != nil {
				allocator = remoteAttachment
			}
			api.Run(apiCtx, serv, cfg, apiOptions, alarmServ, attachments, allocator, controlBlob,
				outbox, controlDownlink, mediaUploadStore, queryer, playbackLibrary)
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
	if attachmentServ != nil {
		attachmentServ.Stop()
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
	if probeServ != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := probeServ.Stop(ctx); err != nil {
			log.Warn().Err(err).Msg("Fail to stop probe server")
		}
		cancel()
	}
	if storeSet != nil {
		if err := storeSet.Close(); err != nil {
			log.Warn().Err(err).Msg("Fail to close database backend")
		}
	}
}

// validate 校验配置文件与二进制的固定角色是否一致。
// 不匹配时错误信息直接给出应该使用的二进制。
func (s Spec) validate(options config.ClusterOptions) error {
	if s.Mode == "" {
		return nil
	}
	expected := s.Mode
	if len(s.RequiredRoles) > 0 {
		roles := append(append([]string{}, s.RequiredRoles...), s.OptionalRoles...)
		expected += " roles=" + strings.Join(roles, ",")
	}
	if options.Mode != s.Mode {
		return fmt.Errorf(
			"%s requires server.mode=%s (%s); current config has mode=%s roles=[%s]. use %s instead",
			s.Binary, s.Mode, expected, options.Mode,
			strings.Join(options.Roles.List(), ","), suggestBinary(options.Roles))
	}
	for _, role := range s.RequiredRoles {
		if !options.Roles.Has(role) {
			return fmt.Errorf(
				"%s requires cluster.roles to contain %q; current roles=[%s]. use %s instead",
				s.Binary, role, strings.Join(options.Roles.List(), ","), suggestBinary(options.Roles))
		}
	}
	allowed := map[string]bool{}
	for _, role := range s.RequiredRoles {
		allowed[role] = true
	}
	for _, role := range s.OptionalRoles {
		allowed[role] = true
	}
	for _, role := range options.Roles.List() {
		if !allowed[role] {
			return fmt.Errorf(
				"%s does not run role %q; current roles=[%s]. use %s instead",
				s.Binary, role, strings.Join(options.Roles.List(), ","), suggestBinary(options.Roles))
		}
	}
	return nil
}

// suggestBinary 根据配置里的角色组合给出应该使用的二进制。
func suggestBinary(roles config.RoleSet) string {
	switch {
	case len(roles) == 0:
		return "jt-server-standalone"
	case roles.Has(config.RoleGateway) && !roles.Has(config.RoleControl) &&
		!roles.Has(config.RoleMedia) && !roles.Has(config.RoleAttachment):
		return "jt-signal"
	case roles.Has(config.RoleControl) && !roles.Has(config.RoleGateway) && !roles.Has(config.RoleMedia):
		return "jt-control"
	case roles.Has(config.RoleMedia) && !roles.Has(config.RoleGateway) &&
		!roles.Has(config.RoleControl) && !roles.Has(config.RoleAttachment):
		return "jt-media"
	default:
		return "jt808-server (generic entry)"
	}
}

// checkResolvableURL 校验 URL 的 host 可以被解析。
func checkResolvableURL(name, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("%s is not a valid URL: %q", name, raw)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("%s has no host: %q", name, raw)
	}
	if _, err := net.LookupHost(host); err != nil {
		return fmt.Errorf("%s host %q is not resolvable: %w", name, host, err)
	}
	return nil
}
