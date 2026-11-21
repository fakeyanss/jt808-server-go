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
	"github.com/fakeyanss/jt808-server-go/internal/business"
	clockpkg "github.com/fakeyanss/jt808-server-go/internal/clock"
	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/control"
	"github.com/fakeyanss/jt808-server-go/internal/controlhttp"
	"github.com/fakeyanss/jt808-server-go/internal/downlinkhttp"
	"github.com/fakeyanss/jt808-server-go/internal/gatewaynode"
	"github.com/fakeyanss/jt808-server-go/internal/internalapi"
	"github.com/fakeyanss/jt808-server-go/internal/media"
	"github.com/fakeyanss/jt808-server-go/internal/media/playback"
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
	"github.com/fakeyanss/jt808-server-go/internal/storage"
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
	adminOptions     config.AdminOptions
	adminEnabled     bool
	apiOptions       config.APIOptions

	remoteMedia      *control.RemoteMedia
	remoteAttachment *control.RemoteAttachment
	// ftpTasks 是 control 侧的 1078 FTP 任务管理器；非集群或未启用时为 nil。
	ftpTasks       *control.FTPTaskManager
	ftpPort        uint16
	ftpAdvertiseIP string

	serv *server.TCPServer
	// runtimeState 是本进程唯一的运行时状态实例，按角色下传给协议、接入与 API 层。
	runtimeState    *runtime.State
	outbox          *control.Outbox
	controlDownlink contracts.DownlinkSender

	retentionPolicy config.RetentionPolicy
	cancelRetention context.CancelFunc
	retentionDones  []<-chan struct{}
	retentionCtx    context.Context

	downlinkServer   *downlinkhttp.Server
	uplinkServer     *controlhttp.Server
	nodeReporter     *gatewaynode.Reporter
	cancelNodeReport context.CancelFunc
	// mediaNodeReporter 是独立 media 进程向 control 注册与心跳的上报器。
	mediaNodeReporter     *medianode.Reporter
	cancelMediaNodeReport context.CancelFunc
	// cancelMediaReconcile 停止媒体端点占用的定期对账。
	cancelMediaReconcile context.CancelFunc
	controlBlob          storage.BlobStore
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
	playbackLibrary playback.PlaybackLibrary

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

	d := &deps{spec: spec, cfg: cfg, settings: s, clock: clockpkg.System(), runtimeState: runtime.NewState()}

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

	// 不变量：集群模式下只有 control 连元数据库。
	// gateway/media/attachment 是数据面节点，不装配任何数据库入口：
	// 网关的档案只读查询走 control 内部 HTTP，其余元数据由 control 独占写入。
	var storeSet *database.Repositories
	if hasControl {
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

	// 协议层只接收仓储接口集合：control 用真实后端，数据面角色用内存实现，
	// 避免把数据库驱动链接进 gateway/media/attachment。
	var protocolRepos repository.Repositories
	if storeSet != nil {
		protocolRepos = storeSet.Repositories
	} else {
		// 数据面角色不连库：用进程内仓储占位，设备档案由 control 在鉴权时落库。
		protocolRepos = memory.NewRepositories()
	}
	processorDeps := protocol.ProcessorDeps{Repos: protocolRepos, Runtime: d.runtimeState, Uplink: &uplinkPolicy}

	// 指标在出口之前创建：超限丢弃等事件需要在网关侧计数。
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
			Attempts: uplink.AttemptOverrides{
				Default:       uplinkSinkOptions.HTTP.MaxAttemptsByClass.Default,
				Location:      uplinkSinkOptions.HTTP.MaxAttemptsByClass.Location,
				BatchLocation: uplinkSinkOptions.HTTP.MaxAttemptsByClass.BatchLocation,
				Alarm:         uplinkSinkOptions.HTTP.MaxAttemptsByClass.Alarm,
				SessionClosed: uplinkSinkOptions.HTTP.MaxAttemptsByClass.SessionClosed,
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
		// cluster 的回调统一由 control 派发：网关只负责投递上行事件，
		// 避免同一事件在 gateway 与 control 各发一次。
		processorDeps.SkipActionHooks = hasGateway && !standalone
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
		d.nodeReporter = reporter
		log.Info().
			Str("gateway_id", clusterOptions.Gateway.NodeID).
			Str("advertise_url", clusterOptions.Gateway.AdvertiseURL).
			Msg("Gateway node reporter enabled")
	}

	var hooks action.Hooks = action.Noop{}
	var actionEvents []string
	if cfg.Server.Action != nil {
		actionEvents = cfg.Server.Action.Events
		if cfg.Server.Action.HTTP != nil && cfg.Server.Action.HTTP.URL != "" {
			hooks = action.NewHTTPCallback(cfg.Server.Action.HTTP.URL, time.Duration(cfg.Server.Action.HTTP.TimeoutMS)*time.Millisecond)
			log.Info().Str("url", cfg.Server.Action.HTTP.URL).Msg("Action HTTP callback enabled")
		}
	}
	// 事件范围装饰在最外层回调之前：内部触发（如自动 0x9208）仍能看到全部事件，
	// 只有对外回调按订阅范围过滤。standalone 与 cluster 共用同一装配。
	filteredHooks, filterErr := action.NewFilter(hooks, actionEvents)
	if filterErr != nil {
		log.Error().Err(filterErr).Msg("Invalid server.action.events")
		os.Exit(1)
	}
	hooks = filteredHooks
	if len(actionEvents) > 0 {
		log.Info().Strs("events", actionEvents).Msg("Action hook event scope configured")
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

	// 单机进程（无上行出口）在本地执行管控业务；集群网关只投递事件，不注入 business。
	if processorDeps.UplinkSink == nil {
		processorDeps.Business = business.New(business.Options{
			Repositories: processorDeps.Repos,
			Hooks:        hooks,
			MediaUpload:  processorDeps.MediaUpload,
		})
	}

	// 0x0100 只写本进程档案缓存，0x0102 随事件带给管控；standalone 同进程也用它补写档案。
	processorDeps.DeviceProfileCache = protocol.NewDeviceProfileCache(5 * time.Minute)
	processorDeps.DeviceStoreAvailable = storeSet != nil
	if uplinkSink != nil {
		// 网关出口才需要消息体上限；standalone/local 不走事件出口。
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
	prefix = strings.TrimSuffix(prefix, internalapi.PathUplink)
	prefix = strings.TrimRight(prefix, "/")
	return base + prefix, nil
}
