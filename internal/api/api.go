package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/alarmserver"
	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/contracts/attachment"
	"github.com/fakeyanss/jt808-server-go/internal/control"
	"github.com/fakeyanss/jt808-server-go/internal/domain"
	"github.com/fakeyanss/jt808-server-go/internal/httpguard"
	"github.com/fakeyanss/jt808-server-go/internal/media/playback"
	"github.com/fakeyanss/jt808-server-go/internal/mediaupload"
	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
	"github.com/fakeyanss/jt808-server-go/internal/server"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

// Queryer 把查询类下行交给网关并等待终端应答体。
// standalone 下为 nil，沿用进程内 broker；cluster 下由管控实现。
type Queryer interface {
	Query(ctx context.Context, phone string, msg model.JT808Msg, expectMsgID uint16, timeout time.Duration) ([]byte, error)
}

// Run 启动业务 API。downlink 为下行契约实现；sender 是豁免清单（查询/只读/幂等）
// 的单发通道，二者在 control-only 下均可跨进程工作。未启用 outbox 时下行入口
// 直接发送，不提供命令状态查询。
func Run(ctx context.Context, serv *server.TCPServer, cfg *config.Config, repos repository.Repositories, runtimeState *runtime.State, apiOptions config.APIOptions, alarmServ *alarmserver.Server, attachments repository.AttachmentFileRepository, attachmentAllocator attachment.AttachmentAllocator, blob storage.BlobStore, downlink contracts.Downlink, sender contracts.DownlinkSender, mediaUpload *mediaupload.Store, queryer Queryer, playback playback.PlaybackLibrary, ftpTasks *control.FTPTaskManager, ftpPort uint16, ftpAdvertiseIP string) {
	// web server structure
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	// 顺序为审计 → 限流 → 鉴权：审计能看到被拒绝的请求，限流在鉴权前挡住洪峰。
	router.Use(httpguard.Audit(), httpguard.RateLimit(apiOptions.RatePerSecond, apiOptions.Burst), httpguard.Auth(apiOptions.Token))
	api := router.Group("/api")
	h := newHandler(serv, cfg, repos, runtimeState, apiOptions, alarmServ, attachments, attachmentAllocator, blob, downlink, sender, mediaUpload, queryer, playback, ftpTasks, ftpPort, ftpAdvertiseIP)

	registerMediaRoutes(api, h.devices, h.repos.MediaFiles, h.repos.MediaCaptures, h.repos.MediaSearches, h.mediaUpload, h.blob)
	registerPlaybackRoutes(api, h.devices, h.connRegistry, h.downlink, h.playback)

	api.GET("/commands/:commandId", h.getCommands)
	api.GET("/device", h.getDevice)
	api.GET("/device/:phone/reports/:kind", h.getDeviceReports)
	api.GET("/device/:phone/geo", h.getDeviceGeo)
	api.GET("/device/:phone/params", h.getDeviceParams)
	api.GET("/device/:phone/properties", h.getDeviceProperties)
	api.GET("/device/:phone/location", h.getDeviceLocation)
	api.POST("/device/:phone/control", h.postDeviceControl)
	api.POST("/device/:phone/regions/delete", h.postDeviceRegionsDelete)
	api.POST("/device/:phone/regions/set", h.postDeviceRegionsSet)
	api.POST("/device/:phone/regions/query", h.postDeviceRegionsQuery)
	api.POST("/device/:phone/driver/query", h.postDeviceDriverQuery)
	api.POST("/device/:phone/recorder/command", h.postDeviceRecorderCommand)
	api.POST("/device/:phone/media/capture", h.postDeviceMediaCapture)
	api.POST("/device/:phone/media/search", h.postDeviceMediaSearch)
	api.POST("/device/:phone/media/resources", h.postDeviceMediaResources)
	api.POST("/device/:phone/media/command", h.postDeviceMediaCommand)
	api.POST("/device/:phone/text/issue", h.postDeviceTextIssue)
	api.POST("/device/:phone/events/set", h.postDeviceEventsSet)
	api.POST("/device/:phone/vehicle/control", h.postDeviceVehicleControl)
	api.POST("/device/:phone/question/issue", h.postDeviceQuestionIssue)
	api.POST("/device/:phone/info/menu/set", h.postDeviceInfoMenuSet)
	api.POST("/device/:phone/info/service", h.postDeviceInfoService)
	api.POST("/device/:phone/call/callback", h.postDeviceCallCallback)
	api.POST("/device/:phone/phonebook/set", h.postDevicePhonebookSet)
	api.POST("/device/:phone/terminal/upgrade", h.postDeviceTerminalUpgrade)
	api.POST("/device/:phone/transparent/down", h.postDeviceTransparentDown)
	api.POST("/device/:phone/alarms/confirm", h.postDeviceAlarmsConfirm)
	api.GET("/device/:phone/alarms", h.getDeviceAlarms)
	api.PUT("/device/:phone/params", h.putDeviceParams)
	api.POST("/device/:phone/attachments/upload", h.postDeviceAttachmentsUpload)
	api.POST("/device/:phone/ftp1078/upload", h.postDeviceFtp1078Upload)
	api.POST("/device/:phone/ftp1078/control", h.postDeviceFtp1078Control)
	api.GET("/device/:phone/ftp1078/task/:serial", h.getDeviceFtp1078Task)
	api.GET("/device/:phone/attachments", h.getDeviceAttachments)
	api.GET("/device/:phone/attachments/:fileName", h.getDeviceAttachment)
	api.GET("/device/:phone/attachments/:fileName/content", h.getDeviceAttachmentsContent)

	httpAddr := ":" + cfg.Server.Port.HTTPPort

	log.Debug().Msgf("Listening and serving HTTP on :%s", cfg.Server.Port.HTTPPort)
	listener, err := net.Listen("tcp", httpAddr)
	if err != nil {
		log.Error().Err(err).Str("addr", httpAddr).Msg("Fail to listen HTTP API addr")
		return
	}
	srv := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		if serveErr := srv.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Error().Err(serveErr).Str("addr", httpAddr).Msg("Fail to run HTTP API server")
		}
	}()

	select {
	case <-ctx.Done():
	case <-serveDone:
		return
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn().Err(err).Msg("Fail to shutdown HTTP API server")
	}
}

// decodeQueryResponse 用终端的协议版本重建报文头并解码查询应答体。
func decodeQueryResponse(device *domain.Device, msgID uint16, body []byte) (model.JT808Msg, error) {
	header := model.GenMsgHeader(device, msgID, 0)
	return protocol.DecodeMessageBody(msgID, header, body)
}

// lookupSessionByPhone 按终端手机号在运行时会话注册表中定位当前活连接。
// SQL 后端不再持久化运行时字段，Device.SessionID 读出来是零值；
// 下行路由因此必须走手机号索引，不能依赖该字段。
func lookupSessionByPhone(ctx context.Context, conn runtime.ConnectionRegistry, device *domain.Device) (*model.Session, error) {
	return conn.GetByPhone(ctx, device.Phone)
}

// localQuerySession 供依赖进程内应答 broker 的查询分支使用。
//
// 这些分支只在 queryer 为空（standalone）时进入：HTTP 请求同步等待本进程
// 收到的应答，必须有本地会话才能发送并注册等待方。
func localQuerySession(ctx context.Context, conn runtime.ConnectionRegistry, device *domain.Device) (*model.Session, error) {
	return lookupSessionByPhone(ctx, conn, device)
}

// downlinkSession 在 outbox 启用时返回 nil：流水号与投递由管控侧完成，
// control-only 没有本地会话也能下发；off 模式仍要求本地会话，保持原有 400 行为。
func downlinkSession(ctx context.Context, conn runtime.ConnectionRegistry, device *domain.Device, outboxEnabled bool) (*model.Session, error) {
	if outboxEnabled {
		return nil, nil
	}
	return lookupSessionByPhone(ctx, conn, device)
}

// nextDownlinkSerial 在无本地会话时返回 0，交给 Downlink 实现分配流水号。
func nextDownlinkSerial(session *model.Session) uint16 {
	if session == nil {
		return 0
	}
	return session.GetNextSerialNum()
}

// dispatchDownlink 通过下行契约下发命令。
// off 模式由实现直接发送并返回 nil 结果，保持既有的 202 语义；
// outbox 启用时流水号留 0 由 Submit 按 (phone, msgId) 原子分配，重投复用同一 serial。
func dispatchDownlink(
	c *gin.Context,
	downlink contracts.Downlink,
	session *model.Session,
	msg model.JT808Msg,
) (*contracts.DownlinkResult, bool, error) {
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if len(idempotencyKey) > 128 {
		return nil, false, errors.New("Idempotency-Key must not exceed 128 characters")
	}
	return dispatchDownlinkWithKey(c, downlink, session, msg, idempotencyKey)
}

// dispatchDownlinkWithKey 与 dispatchDownlink 相同，但幂等键由调用方指定。
// 停机等“同一资源只能下发一次”的场景需要按会话生成确定性幂等键，
// 不能依赖客户端请求头，否则重复请求会各自生成新命令。
func dispatchDownlinkWithKey(
	c *gin.Context,
	downlink contracts.Downlink,
	session *model.Session,
	msg model.JT808Msg,
	idempotencyKey string,
) (*contracts.DownlinkResult, bool, error) {
	if downlink == nil {
		return nil, false, errors.New("command outbox is not configured")
	}
	serial := uint16(0)
	sessionID := ""
	if session != nil {
		sessionID = session.ID
	}
	if header := msg.GetHeader(); header != nil {
		serial = header.SerialNumber
		if downlink.Enabled() {
			// 流水号归属管控：不复用本地会话计数器，避免 control-only 下无会话失败。
			serial = 0
			header.SerialNumber = 0
		}
	}
	result, err := downlink.Submit(c.Request.Context(), contracts.DownlinkRequest{
		SessionID:      sessionID,
		IdempotencyKey: idempotencyKey,
		Serial:         serial,
		Message:        msg,
	})
	if !downlink.Enabled() {
		return nil, false, err
	}
	created := result.ID != "" && !result.Duplicated
	return &result, created, err
}

// writeCommandAccepted 返回下行命令受理结果；幂等命中用 200 与 202 区分。
func writeCommandAccepted(c *gin.Context, command *contracts.DownlinkResult, created bool, fields gin.H) {
	// off 模式没有 outbox 记录，只返回已下发。
	if command == nil {
		c.JSON(http.StatusAccepted, fields)
		return
	}
	fields["commandId"] = command.ID
	fields["duplicate"] = !created
	status := http.StatusAccepted
	if !created {
		status = http.StatusOK
	}
	c.JSON(status, fields)
}
