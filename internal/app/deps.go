package app

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/adminapi"
	"github.com/fakeyanss/jt808-server-go/internal/alarmserver"
	"github.com/fakeyanss/jt808-server-go/internal/attachmentserver"
	clockpkg "github.com/fakeyanss/jt808-server-go/internal/clock"
	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/control"
	"github.com/fakeyanss/jt808-server-go/internal/downlinkhttp"
	"github.com/fakeyanss/jt808-server-go/internal/gatewaynode"
	"github.com/fakeyanss/jt808-server-go/internal/media"
	"github.com/fakeyanss/jt808-server-go/internal/medianode"
	"github.com/fakeyanss/jt808-server-go/internal/mediaupload"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/repository/database"
	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
	"github.com/fakeyanss/jt808-server-go/internal/server"
	"github.com/fakeyanss/jt808-server-go/internal/uplink"
)

// deps 汇总启动过程中构造出的全部组件，供各角色装配与关闭阶段使用。
type deps struct {
	spec     Spec
	cfg      *config.Config
	settings settings

	standalone      bool
	hasGateway      bool
	hasControl      bool
	hasMedia        bool
	hasAttachment   bool
	embeddedStorage bool

	storeSet      *database.Repositories
	commandRepo   repository.CommandRepository
	outboxMode    string
	uplinkPolicy  uplink.Policy
	processorDeps protocol.ProcessorDeps
	metrics       *observability.Metrics
	// clock 是进程内唯一时钟源，所有判定与过期时刻都从这里取。
	clock clockpkg.Clock

	uplinkSink *uplink.HTTPSink
	hooks      action.Hooks

	alarmOptions     config.AlarmServerOptions
	alarmEnabled     bool
	mediaOptions     config.MediaServerOptions
	mediaEnabled     bool
	mediaUploadStore *mediaupload.Store
	authProvider     contracts.AuthProvider
	adminOptions     config.AdminOptions
	adminEnabled     bool
	apiOptions       config.APIOptions

	remoteMedia      *control.RemoteMedia
	remoteAttachment *control.RemoteAttachment
	// ftpTasks 是 control 侧的 1078 FTP 任务管理器；非集群或未启用时为 nil。
	ftpTasks       *control.FTPTaskManager
	ftpPort        uint16
	ftpAdvertiseIP string

	serv            *server.TCPServer
	outbox          *control.Outbox
	controlDownlink contracts.DownlinkSender

	retentionPolicy config.RetentionPolicy
	cancelRetention context.CancelFunc
	retentionDones  []<-chan struct{}
	retentionCtx    context.Context

	downlinkServer   *downlinkhttp.Server
	uplinkServer     *uplink.Server
	nodeReporter     *gatewaynode.Reporter
	cancelNodeReport context.CancelFunc
	// mediaNodeReporter 是独立 media 进程向 control 注册与心跳的上报器。
	mediaNodeReporter     *medianode.Reporter
	cancelMediaNodeReport context.CancelFunc
	// cancelMediaReconcile 停止媒体端点占用的定期对账。
	cancelMediaReconcile context.CancelFunc
	controlBlob          contracts.BlobStore
	businessExecutor     *control.BusinessExecutor
	cancelSweep          context.CancelFunc
	cancelOutbox         context.CancelFunc
	outboxDone           <-chan struct{}
	cancelClockSkew      context.CancelFunc
	clockSkewDone        <-chan struct{}

	udpServ         *server.UDPServer
	alarmServ       *alarmserver.Server
	attachmentServ  *attachmentserver.Server
	mediaServ       *media.Server
	playbackLibrary contracts.PlaybackLibrary

	probes    *observability.Provider
	adminServ *adminapi.Server
	probeServ *observability.ProbeServer

	apiCtx    context.Context
	cancelAPI context.CancelFunc
	apiDone   chan struct{}

	cancelUplinkSink context.CancelFunc
	uplinkSinkDone   <-chan struct{}
}

// buildDeps 构造与角色无关的依赖：配置解析结果、数据库、出口、outbox 与各角色公共组件。
// 只构造，不等待信号；后续按角色装配由 gateway/control/media/attachment/api 文件完成。
func buildDeps(spec Spec, cfg *config.Config, s settings) *deps {
	outboxOptions := s.outbox
	uplinkOptions := s.uplink
	uplinkSinkOptions := s.uplinkSink
	clusterOptions := s.options

	d := &deps{spec: spec, cfg: cfg, settings: s, clock: clockpkg.System()}

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

	// 只有 gateway/control 需要共享库；media/attachment 是数据面节点，不连数据库。
	var storeSet *database.Repositories
	if hasGateway || hasControl {
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
			commandRepo = memory.NewCommandRepositoryWithClock(d.clock)
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

	processorDeps := protocol.ProcessorDeps{Repos: storeSet, Runtime: runtime.Default(), Uplink: &uplinkPolicy}

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
			OnFenced: func(phone, sessionID string) {
				// best-effort 事件被 control 围栏：立即关闭本地会话，不再接收该 phone 的上行。
				session, lookupErr := processorDeps.Runtime.Conn.Get(context.Background(), sessionID)
				if lookupErr != nil || session == nil {
					return
				}
				session.ClosePending()
				if session.Conn != nil {
					_ = session.Conn.Close()
				}
				processorDeps.Runtime.Conn.Unbind(context.Background(), sessionID)
			},
		})
		if err != nil {
			log.Error().Err(err).Msg("Fail to initialize uplink HTTP sink")
			os.Exit(1)
		}
		uplinkSink = sink
		processorDeps.UplinkSink = sink
		processorDeps.GatewayID = uplinkSinkOptions.HTTP.GatewayID
		log.Info().
			Str("url", uplinkSinkOptions.HTTP.URL).
			Dur("timeout", uplinkSinkOptions.HTTP.Timeout).
			Int("queue_size", uplinkSinkOptions.HTTP.QueueSize).
			Int("shards", uplinkSinkOptions.HTTP.Workers).
			Int("max_attempts", uplinkSinkOptions.HTTP.MaxAttempts).
			Str("gateway_id", uplinkSinkOptions.HTTP.GatewayID).
			Msg("Uplink HTTP sink enabled")
	} else {
		log.Info().Str("mode", uplinkSinkOptions.Mode).Msg("Uplink sink configured")
	}

	// 集群网关把节点状态登记到 control；standalone 或非 HTTP 出口不注册。
	if hasGateway && !standalone && uplinkSinkOptions.Mode == config.UplinkSinkHTTP {
		baseURL, baseErr := controlBaseURL(uplinkSinkOptions.HTTP.URL)
		if baseErr != nil {
			log.Error().Err(baseErr).Str("uplink_url", uplinkSinkOptions.HTTP.URL).
				Msg("Fail to derive control base URL for gateway node registration")
			os.Exit(1)
		}
		instanceID := gatewaynode.NewInstanceID()
		reporter, reporterErr := gatewaynode.NewReporter(gatewaynode.ReporterOptions{
			BaseURL:     baseURL,
			Token:       uplinkSinkOptions.HTTP.Token,
			GatewayID:   clusterOptions.Gateway.NodeID,
			InstanceID:  instanceID,
			DownlinkURL: clusterOptions.Gateway.AdvertiseURL,
			Interval:    gatewaynode.HeartbeatInterval,
		})
		if reporterErr != nil {
			log.Error().Err(reporterErr).Msg("Fail to create gateway node reporter")
			os.Exit(1)
		}
		// 上行信封的网关身份必须与节点注册一致，control 才能把 claim 落到正确节点。
		processorDeps.GatewayID = clusterOptions.Gateway.NodeID
		processorDeps.InstanceID = instanceID
		// 会话断开时上报 release；失败不影响连接关闭，目录残留由节点过期兜底。
		processorDeps.OnSessionClosed = func(sessionID, phone string, generation uint64) {
			if phone == "" || generation == 0 {
				return
			}
			go func() {
				releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if err := reporter.Release(releaseCtx, phone, sessionID, generation); err != nil {
					log.Warn().Err(err).Str("phone", phone).Str("session", sessionID).
						Msg("Session release failed; directory will expire with node liveness")
				}
			}()
		}
		d.nodeReporter = reporter
		log.Info().
			Str("gateway_id", clusterOptions.Gateway.NodeID).
			Str("advertise_url", clusterOptions.Gateway.AdvertiseURL).
			Msg("Gateway node reporter enabled")
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
		processorDeps.MediaUpload = mediaUploadStore
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
	processorDeps.AuthProvider = authProvider
	if uplinkSink != nil {
		// 网关出口才需要本地注册缓存与消息体上限；standalone/local 不走事件出口。
		processorDeps.RegisterCache = protocol.NewRegisterCache(5 * time.Minute)
		processorDeps.UplinkMaxBodyBytes = clusterOptions.Uplink.MaxBodyBytes
		processorDeps.OnOversized = func(msgID uint16, phone string, size int) {
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
		remoteMedia = control.NewRemoteMedia(control.RemoteMediaOptions{
			Nodes:    nodes,
			NodeRepo: storeSet.MediaNodes,
			Clock:    d.clock,
			Metrics:  metrics,
		})
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
		serv.SetDeps(processorDeps)
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
				Conn: processorDeps.Runtime.Conn,
				Send: func(sessionID string, msg model.JT808Msg, serial uint16) error {
					return serv.Send(sessionID, msg, server.WithSerial(serial))
				},
			})
			if senderErr != nil {
				log.Error().Err(senderErr).Msg("Fail to create local downlink sender")
				os.Exit(1)
			}
			downlinkSender = sender
			log.Info().Str("downlink_mode", "local").Msg("Local downlink sender enabled")
		} else if clusterOptions.Gateway.Routing == config.GatewayRoutingFixed {
			// fixed：单节点直连，固定入口，不查会话目录。
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
			log.Info().
				Str("downlink_mode", "fixed").
				Str("gateway_url", clusterOptions.Gateway.URL).
				Msg("HTTP downlink client enabled")
		} else {
			// directory：按 phone 的会话目录定位归属网关，节点不可用一律 offline。
			router, routerErr := downlinkhttp.NewRouter(downlinkhttp.RouterOptions{
				Directory: storeSet.SessionDirectory,
				Token:     clusterOptions.Gateway.Token,
				Timeout:   clusterOptions.Downlink.Timeout,
				Metrics:   metrics,
				Clock:     d.clock,
			})
			if routerErr != nil {
				log.Error().Err(routerErr).Msg("Fail to create session directory downlink router")
				os.Exit(1)
			}
			downlinkSender = router
			log.Info().Str("downlink_mode", "directory").Msg("Session directory downlink router enabled")
		}
		built, buildErr := control.NewOutbox(control.OutboxDependencies{
			Commands: commandRepo,
			Devices:  storeSet.Devices,
			Metrics:  metrics,
			Send:     downlinkSender,
			Clock:    d.clock,
		}, control.OutboxOptions{
			Enabled:                outboxMode != config.CommandOutboxModeOff,
			InitialWait:            outboxOptions.InitialWait,
			ExpireAfter:            outboxOptions.ExpireAfter,
			PollInterval:           outboxOptions.PollInterval,
			BatchSize:              outboxOptions.BatchSize,
			MaxAttempts:            outboxOptions.MaxAttempts,
			LeaseDuration:          outboxOptions.LeaseDuration,
			LeaseTolerance:         clusterOptions.LeaseTolerance,
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

	d.standalone = standalone
	d.hasGateway = hasGateway
	d.hasControl = hasControl
	d.hasMedia = hasMedia
	d.hasAttachment = hasAttachment
	d.embeddedStorage = embeddedStorage
	d.storeSet = storeSet
	d.commandRepo = commandRepo
	d.outboxMode = outboxMode
	d.uplinkPolicy = uplinkPolicy
	d.processorDeps = processorDeps
	d.metrics = metrics
	d.uplinkSink = uplinkSink
	d.hooks = hooks
	d.alarmOptions = alarmOptions
	d.alarmEnabled = alarmEnabled
	d.mediaOptions = mediaOptions
	d.mediaEnabled = mediaEnabled
	d.mediaUploadStore = mediaUploadStore
	d.authProvider = authProvider
	d.adminOptions = adminOptions
	d.adminEnabled = adminEnabled
	d.apiOptions = apiOptions
	d.remoteMedia = remoteMedia
	d.remoteAttachment = remoteAttachment
	d.serv = serv
	d.outbox = outbox
	d.controlDownlink = controlDownlink
	return d
}

// controlBaseURL 从上行出口地址推导 control 内部接口的基址。
// uplink.http.url 指向上行入口，节点注册/心跳与它同源，去掉路径后缀即可复用同一地址与 token。
func controlBaseURL(endpoint string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid uplink endpoint %q", endpoint)
	}
	base := parsed.Scheme + "://" + parsed.Host
	prefix := strings.TrimRight(parsed.Path, "/")
	prefix = strings.TrimSuffix(prefix, uplink.PathUplink)
	prefix = strings.TrimRight(prefix, "/")
	return base + prefix, nil
}
