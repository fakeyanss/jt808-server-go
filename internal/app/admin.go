package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/adminapi"
	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/control"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

// buildProbes 汇总就绪检查与指标快照。
func (d *deps) buildProbes() {
	storeSet := d.storeSet
	embeddedStorage := d.embeddedStorage
	outbox := d.outbox
	hasMedia := d.hasMedia
	mediaEnabled := d.mediaEnabled
	mediaOptions := d.mediaOptions
	hasAttachment := d.hasAttachment
	alarmEnabled := d.alarmEnabled
	standalone := d.standalone
	alarmOptions := d.alarmOptions
	controlBlob := d.controlBlob
	mediaUploadStore := d.mediaUploadStore
	hasControl := d.hasControl
	hasGateway := d.hasGateway
	uplinkSinkOptions := d.settings.uplinkSink
	remoteMedia := d.remoteMedia
	remoteAttachment := d.remoteAttachment
	processorDeps := d.processorDeps
	metrics := d.metrics
	mediaServ := d.mediaServ
	uplinkSink := d.uplinkSink
	alarmServ := d.alarmServ
	clock := d.clock

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
					if controlBlob == nil {
						return fmt.Errorf("attachment object store is not configured")
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
			snapshot := observability.Snapshot{ConnectedSessions: processorDeps.Runtime.Conn.Count(ctx)}
			for _, binding := range processorDeps.Runtime.Conn.ListBindings(ctx) {
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
			// 节点存活水位：过期判定用进程时钟，多副本口径一致。
			if storeSet != nil && storeSet.GatewayNodes != nil {
				stats, err := storeSet.GatewayNodes.Stats(ctx, clock.Now())
				if err != nil {
					log.Warn().Err(err).Msg("Fail to collect gateway node metrics")
				} else {
					snapshot.GatewayNodes = observability.GatewayNodeSnapshot{
						Online:  stats.Online,
						Offline: stats.Offline,
						Expired: stats.Expired,
					}
				}
			}
			// 媒体节点负载：心跳上报的小指标，按节点输出便于定位热点。
			if storeSet != nil && storeSet.MediaNodes != nil {
				nodes, err := storeSet.MediaNodes.ListOnline(ctx, clock.Now())
				if err != nil {
					log.Warn().Err(err).Msg("Fail to collect media node metrics")
				}
				for _, node := range nodes {
					snapshot.MediaNodes = append(snapshot.MediaNodes, observability.MediaNodeSnapshot{
						NodeID:             node.NodeID,
						ActiveChannels:     node.ActiveChannels,
						InboundBitrateKbps: node.InboundBitrateKbps,
						EndpointsUsed:      node.EndpointsUsed,
						EndpointCapacity:   node.EndpointCapacity,
					})
				}
			}
			if uplinkSink != nil {
				stats := uplinkSink.Stats()
				snapshot.Uplink = observability.UplinkSnapshot{
					Delivered:   stats.Delivered,
					Failed:      stats.Failed,
					Timeouts:    stats.Timeouts,
					Dropped:     stats.Dropped,
					Rejected:    stats.Rejected,
					InFlight:    stats.InFlight,
					ShardQueues: stats.ShardQueues,
				}
			}
			return snapshot
		},
	}

	d.probes = probes
}

// startAdmin 启动管理面。
func (d *deps) startAdmin() {
	hasControl := d.hasControl
	adminEnabled := d.adminEnabled
	adminOptions := d.adminOptions
	storeSet := d.storeSet
	processorDeps := d.processorDeps
	probes := d.probes

	var adminServ *adminapi.Server
	if hasControl && adminEnabled {
		adminServ = adminapi.NewServer(adminOptions, adminapi.Dependencies{
			Conn:      processorDeps.Runtime.Conn,
			Heartbeat: processorDeps.Runtime.Heartbeat,
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

	d.adminServ = adminServ
}

// startProbes 启动存活/就绪探针。
func (d *deps) startProbes() {
	probeEnabled := d.settings.probeEnabled
	probeAddr := d.settings.probeAddr
	probes := d.probes

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

	d.probeServ = probeServ
}
