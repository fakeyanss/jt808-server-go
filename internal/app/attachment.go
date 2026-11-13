package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/alarmserver"
	"github.com/fakeyanss/jt808-server-go/internal/attachmentnode"
	"github.com/fakeyanss/jt808-server-go/internal/attachmentserver"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/control"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

// attachmentControlBase 从进度上报地址反推 control 基址，复用同一 token。
func attachmentControlBase(progressURL string) string {
	return strings.TrimSuffix(strings.TrimSpace(progressURL), "/internal/v1/attachment/progress")
}

// advertiseHost 选择节点对 control 通告的主机名。
func advertiseHost(ftpAdvertiseIP, uploadIP string) string {
	for _, candidate := range []string{ftpAdvertiseIP, uploadIP} {
		if host := strings.TrimSpace(candidate); host != "" {
			return host
		}
	}
	return "127.0.0.1"
}

// errAttachmentBlobRequired 表示附件角色缺少共享对象存储。
// 前置依赖：config.ValidateServerMode 对 media/attachment 角色强制
// cluster.storage.backend ∈ {sharedVolume, objectStore}（见 config.go 的 needsBlob 分支）；
// 这两种后端都会在 buildDeps 里给 controlBlob 赋值。因此正常启动走不到这里，
// 本守卫只用于防御将来放宽校验、或直接构造依赖时把 nil 传进 attachmentserver.New。
var errAttachmentBlobRequired = errors.New("attachment role requires cluster.storage.backend=objectStore (accepted values: local|sharedVolume|objectStore; attachment role only supports objectStore) with a shared object store")

// checkAttachmentBlob 校验附件角色是否具备共享对象存储依赖。
func checkAttachmentBlob(blob contracts.BlobStore) error {
	if blob == nil {
		return errAttachmentBlobRequired
	}
	return nil
}

// attachmentProgressSink 选择附件节点上报进度的通道。
//
// 同进程有 control 时直接写库；独立部署时用出站 HTTP，禁止 control 反向连节点。
// 都没配置时返回 nil：接收与对象写入照常进行，恢复改用对象存储扫描。
func (d *deps) attachmentProgressSink() (contracts.AttachmentProgressSink, error) {
	if d.hasControl && d.storeSet != nil {
		if store := control.NewAttachmentProgressStore(d.storeSet.AttachmentFiles, d.clock); store != nil {
			return store, nil
		}
	}
	report := d.alarmOptions.ProgressReport
	if report.URL == "" {
		log.Warn().Msg("alarmServer.progressReport.url is not configured; attachment progress relies on object storage scan")
		return nil, nil
	}
	return attachmentserver.NewHTTPProgressSink(report.URL, report.Token, report.Timeout)
}

// installUploadHooks 按形态安装 0x9208 自动上传触发。
func (d *deps) installUploadHooks() {
	alarmOptions := d.alarmOptions
	standalone := d.standalone
	hasControl := d.hasControl
	spec := d.spec
	clusterOptions := d.settings.options
	metrics := d.metrics
	hooks := d.hooks
	authProvider := d.authProvider
	processorDeps := d.processorDeps
	serv := d.serv

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
			session, err := processorDeps.Runtime.Conn.GetByPhone(ctx, device.Phone)
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

	d.hooks = hooks
}

// startAttachment 装配附件节点：standalone 写本地目录，cluster 切片写共享对象存储。
func (d *deps) startAttachment() {
	hasAttachment := d.hasAttachment
	alarmEnabled := d.alarmEnabled
	standalone := d.standalone
	alarmOptions := d.alarmOptions
	clusterOptions := d.settings.options
	controlBlob := d.controlBlob
	hasControl := d.hasControl
	remoteAttachment := d.remoteAttachment

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
		// cluster 附件节点：本地只保留有界暂存，切片写入共享 objectStore；
		// 水位、段列表与终态由 control 落库，本进程不持有任何仓储。
		if err := checkAttachmentBlob(controlBlob); err != nil {
			log.Error().Msg(err.Error())
			os.Exit(1)
		}
		progress, progressErr := d.attachmentProgressSink()
		if progressErr != nil {
			log.Error().Err(progressErr).Msg("Fail to configure attachment progress report")
			os.Exit(1)
		}
		nodeID := clusterOptions.Attachment.NodeID
		if nodeID == "" {
			nodeID = fmt.Sprintf("%s:%d", alarmOptions.UploadIP, alarmOptions.Port)
		}
		var ftpConfig *attachmentserver.FTPConfig
		ftpAdvertiseIP := ""
		controlBase := attachmentControlBase(alarmOptions.ProgressReport.URL)
		if clusterOptions.Attachment.FTP.Enabled {
			ftpCfg := clusterOptions.Attachment.FTP
			ftpAdvertiseIP = strings.TrimSpace(ftpCfg.AdvertiseIP)
			if ftpAdvertiseIP == "" {
				ftpAdvertiseIP = strings.TrimSpace(alarmOptions.UploadIP)
			}
			ftpConfig = &attachmentserver.FTPConfig{
				Port: ftpCfg.Port, PassivePorts: ftpCfg.PassivePorts, AdvertiseIP: ftpAdvertiseIP,
				MaxFileSize: alarmOptions.MaxFileSize, SegmentSize: alarmOptions.SegmentSizeBytes,
			}
			if ftpAdvertiseIP == "" {
				log.Error().Msg("ftp1078 is enabled but no advertise ip is available (set cluster.attachment.ftp1078.advertiseIp or alarmServer.uploadIp)")
				os.Exit(1)
			}
		}
		advertiseURL := strings.TrimSpace(clusterOptions.Attachment.AdvertiseURL)
		if advertiseURL == "" && alarmOptions.HTTPPort != 0 {
			advertiseURL = fmt.Sprintf("http://%s:%d", advertiseHost(ftpAdvertiseIP, alarmOptions.UploadIP), alarmOptions.HTTPPort)
		}
		built, err := attachmentserver.New(attachmentserver.Config{
			Dir:              alarmOptions.Dir,
			TCPPort:          alarmOptions.Port,
			UDPPort:          alarmOptions.UDPPort,
			AdvertiseIP:      alarmOptions.UploadIP,
			NodeID:           nodeID,
			MaxFileSize:      alarmOptions.MaxFileSize,
			MaxConcurrent:    alarmOptions.MaxConcurrent,
			SessionTimeout:   alarmserver.DefaultSessionTimeout,
			CompletedTTL:     alarmOptions.CompletedTTL,
			Retention:        alarmOptions.Retention,
			Blob:             controlBlob,
			Progress:         progress,
			StagingDir:       filepath.Join(alarmOptions.Dir, "staging"),
			SegmentSize:      alarmOptions.SegmentSizeBytes,
			MaxStagingBytes:  alarmOptions.StagingMaxBytes,
			HTTPAddr:         fmt.Sprintf(":%d", alarmOptions.HTTPPort),
			FTP:              ftpConfig,
			ControlBaseURL:   controlBase,
			ControlToken:     alarmOptions.ProgressReport.Token,
			ControlPushToken: clusterOptions.Control.Token,
			InstanceID:       attachmentnode.NewInstanceID(),
			AdvertiseURL:     advertiseURL,
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
		if ftpConfig != nil {
			if err := built.ListenFTP(fmt.Sprintf(":%d", ftpConfig.Port)); err != nil {
				log.Error().Err(err).Uint16("port", ftpConfig.Port).Msg("Fail to listen ftp1078 control addr")
				os.Exit(1)
			}
			log.Info().Str("addr", built.FTPAddr()).Str("advertise_ip", ftpConfig.AdvertiseIP).
				Msg("1078 FTP upload service listening")
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

	d.alarmServ = alarmServ
	d.attachmentServ = attachmentServ
	d.remoteAttachment = remoteAttachment
}
