package app

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/alarmserver"
	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/control"
	"github.com/fakeyanss/jt808-server-go/internal/coordination"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/fakeyanss/jt808-server-go/internal/uplink"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

// setupControl 装配控制面：共享对象存储、上行 HTTP 入口与 pending 对象巡检。
func (d *deps) setupControl() {
	hasControl := d.hasControl
	standalone := d.standalone
	clusterOptions := d.settings.options
	outbox := d.outbox
	uplinkPolicy := d.uplinkPolicy
	metrics := d.metrics
	mediaUploadStore := d.mediaUploadStore
	hooks := d.hooks
	alarmOptions := d.alarmOptions
	remoteAttachment := d.remoteAttachment
	controlDownlink := d.controlDownlink
	storeSet := d.storeSet
	processorDeps := d.processorDeps
	// 数据面节点（media/attachment）没有共享库，也不参与租约；只有 control 需要。
	var leaseRunner *coordination.Runner
	if storeSet != nil {
		leaseRunner = coordination.NewRunner(storeSet.Coordination, coordination.DefaultTTL, d.clock, clusterOptions.LeaseTolerance)
	}

	var uplinkServer *uplink.Server
	var controlBlob contracts.BlobStore
	var businessExecutor *control.BusinessExecutor
	var ftpManager *control.FTPTaskManager
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
			Clock:               d.clock,
		})
		if srvErr != nil {
			log.Error().Err(srvErr).Msg("Fail to create uplink HTTP server")
			os.Exit(1)
		}
		if outbox != nil {
			srv.SetTerminalReplyHandler(outbox)
		}
		// 1078 FTP 上传任务：元数据全在 control，附件节点只出站鉴权与上报进度。
		ftpManager = control.NewFTPTaskManager(control.FTPTaskOptions{
			Tasks: storeSet.AttachmentFTPTasks,
			Nodes: storeSet.AttachmentNodes,
			Pusher: &control.FTPControlPusher{
				Nodes: storeSet.AttachmentNodes, Token: clusterOptions.Control.Token,
				Clock: d.clock, Metrics: metrics,
			},
			Clock: d.clock, Metrics: metrics, MaxFileSize: alarmOptions.MaxFileSize,
		})
		if ftpManager != nil {
			srv.SetAttachmentFTPSessionStore(ftpManager)
			srv.SetAttachmentNodeRepository(storeSet.AttachmentNodes)
			d.ftpTasks = ftpManager
			d.ftpPort = clusterOptions.Attachment.FTP.Port
			d.ftpAdvertiseIP = clusterOptions.Attachment.FTP.AdvertiseIP
			if d.ftpAdvertiseIP == "" {
				d.ftpAdvertiseIP = alarmOptions.UploadIP
			}
		}
		// 控制面自己识别报警并下发 0x9208：附件节点地址来自 HTTP 节点状态，
		// 不再要求 gateway 与 control 同进程。
		controlHooks := hooks
		if alarmOptions.AutoUpload && hasControl && remoteAttachment != nil && controlDownlink != nil {
			controlHooks = control.NewAutoUploadTrigger(hooks, remoteAttachment, controlDownlink, storeSet.Coordination, 30*time.Minute, d.clock)
			log.Info().Msg("Automatic 0x9208 attachment upload trigger enabled in control via remote attachment nodes")
		}
		executor := control.NewBusinessExecutor(control.BusinessOptions{
			ProcessorDeps: processorDeps,
			Devices:       storeSet.Devices,
			Sessions:      storeSet.SessionDirectory,
			Files:         storeSet.MediaFiles,
			Segments:      storeSet.RecordingSegments,
			Blob:          controlBlob,
			MediaUpload:   mediaUploadStore,
			Hooks:         controlHooks,
			Clock:         d.clock,
			OnUnknownDevice: func(phone string) {
				log.Error().Str("phone", phone).Msg("Uplink event rejected: device is not registered")
			},
			OnMediaTypeRejected: func(phone string, mediaType uint8) {
				metrics.IncMediaTypeRejected()
			},
			OnStorageCommitFailed: func(phone string, mediaID uint32) {
				metrics.IncStorageCommitFailed()
			},
			OnMediaUploadNotice: func(ctx context.Context, phone string, notice *model.Msg1206) {
				if ftpManager != nil && notice != nil {
					ftpManager.CompleteFromNotice(ctx, phone, notice.AnswerSerialNumber, notice.Result)
				}
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
		srv.SetGatewayNodeRepository(storeSet.GatewayNodes)
		srv.SetSessionDirectoryRepository(storeSet.SessionDirectory)
		srv.SetRecordingSegmentRepository(storeSet.RecordingSegments)
		// 媒体节点注册表：0x9101/0x9201 从这里按负载与容量选点，静态清单只作兜底。
		srv.SetMediaNodeRepository(storeSet.MediaNodes)
		// 附件节点不连库：进度上报与恢复点查询都走这个入口。
		if progressStore := control.NewAttachmentProgressStore(storeSet.AttachmentFiles, d.clock); progressStore != nil {
			srv.SetAttachmentProgressStore(progressStore)
		}
		srv.SetMetrics(metrics)
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
		indexer := control.NewRecordingIndexer(storeSet.RecordingSegments, controlBlob)
		// 附件索引维护：附件节点不连库，过期标记、pending 回收、保留期与扫描补齐都在 control。
		attachmentMaint := control.NewAttachmentMaintenance(storeSet.AttachmentFiles, controlBlob,
			control.AttachmentMaintenanceOptions{
				SessionTimeout: alarmserver.DefaultSessionTimeout,
				Retention:      alarmOptions.Retention,
				Metrics:        metrics,
			}, d.clock)
		runAttachmentMaintenance := func(ctx context.Context) {
			if attachmentMaint == nil {
				return
			}
			if expired, err := attachmentMaint.ExpireReceiving(ctx); err != nil {
				log.Warn().Err(err).Msg("Expire stale attachment receiving records failed")
			} else if expired > 0 {
				log.Info().Int("expired", expired).Msg("Marked stale attachment receiving records expired")
			}
			attachmentMaint.SweepPending(ctx)
			if removed, err := attachmentMaint.PruneRetention(ctx); err != nil {
				log.Warn().Err(err).Msg("Prune expired attachment records failed")
			} else if removed > 0 {
				log.Info().Int("removed", removed).Msg("Pruned expired attachment records and objects")
			}
		}
		runAttachmentBackfill := func(ctx context.Context) {
			if attachmentMaint == nil {
				return
			}
			if _, err := attachmentMaint.Backfill(ctx); err != nil {
				log.Warn().Err(err).Msg("Backfill attachment index failed")
			}
		}
		runFTPTaskExpiry := func(ctx context.Context) {
			if ftpManager == nil {
				return
			}
			if expired, err := ftpManager.Expire(ctx); err != nil {
				log.Warn().Err(err).Msg("Expire ftp upload tasks failed")
			} else if expired > 0 {
				log.Info().Int("expired", expired).Msg("Expired ftp upload tasks and revoked credentials")
			}
		}
		routines.GoSafe(func() {
			if indexer != nil {
				leaseRunner.Run(sweepCtx, "media-segment-index", func(ctx context.Context) {
					if _, err := indexer.Backfill(ctx); err != nil {
						log.Warn().Err(err).Msg("Backfill media segment index failed")
					}
				})
			}
			leaseRunner.Run(sweepCtx, "attachment-maintenance", runAttachmentMaintenance)
			leaseRunner.Run(sweepCtx, "attachment-index", runAttachmentBackfill)
			leaseRunner.Run(sweepCtx, "attachment-ftp-expiry", runFTPTaskExpiry)
			ticker := time.NewTicker(10 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-sweepCtx.Done():
					return
				case <-ticker.C:
					leaseRunner.Run(sweepCtx, "media-pending-sweep", func(ctx context.Context) {
						businessExecutor.SweepPending(ctx, 30*time.Minute)
						if _, err := storeSet.Coordination.PruneExpired(ctx, d.clock.Now()); err != nil {
							log.Warn().Err(err).Msg("Prune expired coordination rows failed")
						}
					})
					if indexer != nil {
						leaseRunner.Run(sweepCtx, "media-segment-index", func(ctx context.Context) {
							if _, err := indexer.Backfill(ctx); err != nil {
								log.Warn().Err(err).Msg("Backfill media segment index failed")
							}
						})
					}
					leaseRunner.Run(sweepCtx, "attachment-maintenance", runAttachmentMaintenance)
					leaseRunner.Run(sweepCtx, "attachment-index", runAttachmentBackfill)
					leaseRunner.Run(sweepCtx, "attachment-ftp-expiry", runFTPTaskExpiry)
				}
			}
		})
	}

	d.controlBlob = controlBlob
	d.uplinkServer = uplinkServer
	d.businessExecutor = businessExecutor
	d.cancelSweep = cancelSweep
	d.remoteAttachment = remoteAttachment
	d.ftpTasks = ftpManager
}

// startUplinkHTTP 在后台启动上行 HTTP 入口。
func (d *deps) startUplinkHTTP() {
	if d.uplinkServer != nil {
		srv := d.uplinkServer
		routines.GoSafe(func() { _ = srv.Start() })
	}
}

// startOutbox 启动命令重投调度，并把可靠投递结果同时喂给下行在途表与 outbox。
func (d *deps) startOutbox() {
	outbox := d.outbox
	serv := d.serv
	downlinkServer := d.downlinkServer

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

	d.cancelOutbox = cancelOutbox
	d.outboxDone = outboxDone
}
