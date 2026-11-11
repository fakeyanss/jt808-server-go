package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
)

const (
	defaultPlaybackTTL = 10 * time.Minute
	maxPlaybackTTL     = time.Hour

	playbackKindLive     = "live"
	playbackKindTerminal = "terminal_playback"
)

type createPlaybackRequest struct {
	Channel    uint8  `json:"channel"`
	Kind       string `json:"kind"`
	TTLSeconds int    `json:"ttlSeconds"`

	// 以下仅 terminal_playback 使用。
	StartTime     string `json:"startTime"`
	EndTime       string `json:"endTime"`
	MediaType     uint8  `json:"mediaType"`
	StreamType    uint8  `json:"streamType"`
	StorageType   uint8  `json:"storageType"`
	PlaybackMode  uint8  `json:"playbackMode"`
	PlaybackSpeed uint8  `json:"playbackSpeed"`
}

// registerPlaybackRoutes 注册播放会话与平台录像 API。
//
// 终端回放（0x9201）与平台录像回放是两件事：前者让终端推 1078 流，
// 后者按 recording_segments 索引 + Range 直接读对象存储，不经过 0x9201。
func registerPlaybackRoutes(api *gin.RouterGroup, devices repository.DeviceRepository, conn runtime.ConnectionRegistry, downlink contracts.Downlink, playback contracts.PlaybackLibrary) {
	outboxEnabled := downlink != nil && downlink.Enabled()

	api.POST("/device/:phone/playback", func(c *gin.Context) {
		if playback == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"err": "media playback is not available on this role"})
			return
		}
		var request createPlaybackRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		kind := request.Kind
		if kind == "" {
			kind = string(playbackKindLive)
		}
		if kind != string(playbackKindLive) && kind != string(playbackKindTerminal) {
			c.JSON(http.StatusBadRequest, gin.H{"err": "kind must be live or terminal_playback"})
			return
		}
		ttl := time.Duration(request.TTLSeconds) * time.Second
		if ttl <= 0 {
			ttl = defaultPlaybackTTL
		}
		if ttl > maxPlaybackTTL {
			c.JSON(http.StatusBadRequest, gin.H{"err": "ttlSeconds must not exceed 3600"})
			return
		}

		var start, end time.Time
		if kind == string(playbackKindTerminal) {
			var err error
			start, err = time.Parse(time.RFC3339, request.StartTime)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"err": "startTime must be RFC3339"})
				return
			}
			end, err = time.Parse(time.RFC3339, request.EndTime)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"err": "endTime must be RFC3339"})
				return
			}
			if !start.Before(end) {
				c.JSON(http.StatusBadRequest, gin.H{"err": "startTime must be before endTime"})
				return
			}
			if request.PlaybackMode > 4 || request.PlaybackSpeed > 5 {
				c.JSON(http.StatusBadRequest, gin.H{"err": "playbackMode or playbackSpeed is out of range"})
				return
			}
		}

		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), conn, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		created, err := playback.CreatePlaybackSession(c.Request.Context(), contracts.PlaybackSessionRequest{
			Phone:   phone,
			Channel: request.Channel,
			Kind:    kind,
			TTL:     ttl,
		})
		if err != nil {
			var conflict *contracts.PlaybackSessionConflictError
			if errors.As(err, &conflict) {
				c.JSON(http.StatusConflict, gin.H{
					"code":    conflict.Code(),
					"message": conflict.Error(),
					"existing": gin.H{
						"sessionId": conflict.Existing.ID,
						"kind":      conflict.Existing.Kind,
						"expiresAt": conflict.Existing.ExpiresAt,
					},
				})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}

		var msg model.JT808Msg
		messageID := "0x9101"
		if kind == string(playbackKindTerminal) {
			messageID = "0x9201"
			msg = &model.Msg9201{
				Header:         model.GenMsgHeader(device, 0x9201, nextDownlinkSerial(session)),
				ServerIP:       created.Endpoint.IP,
				TCPPort:        created.Endpoint.TCPPort,
				UDPPort:        created.Endpoint.UDPPort,
				LogicChannelID: request.Channel,
				MediaType:      request.MediaType,
				StreamType:     request.StreamType,
				StorageType:    request.StorageType,
				PlaybackMode:   request.PlaybackMode,
				PlaybackSpeed:  request.PlaybackSpeed,
				StartTime:      &start,
				EndTime:        &end,
			}
		} else {
			msg = &model.Msg9101{
				Header:         model.GenMsgHeader(device, 0x9101, nextDownlinkSerial(session)),
				ServerIP:       created.Endpoint.IP,
				TCPPort:        created.Endpoint.TCPPort,
				UDPPort:        created.Endpoint.UDPPort,
				LogicChannelID: request.Channel,
				DataType:       request.MediaType,
				StreamType:     request.StreamType,
			}
		}
		command, commandCreated, err := dispatchDownlink(c, downlink, session, msg)
		if err != nil {
			// 信令没发出去就不能留下孤儿会话，否则同一通道会被一直占住。
			_ = playback.ReleasePlaybackSession(c.Request.Context(), created.ID)
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		fields := gin.H{
			"messageId":   messageID,
			"sessionId":   created.ID,
			"channel":     created.Channel,
			"kind":        created.Kind,
			"endpoints":   toEndpointJSON(created.Endpoints),
			"playbackUrl": created.PlaybackURL,
			"output":      created.Output,
			"expiresAt":   created.ExpiresAt,
		}
		writeCommandAccepted(c, command, commandCreated, fields)
	})

	// DELETE 同一会话必须幂等：先同步串行化，避免并发 DELETE 各发一次停止指令。
	var stopMu sync.Mutex
	api.DELETE("/playback/:sessionId", func(c *gin.Context) {
		if playback == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"err": "media playback is not available on this role"})
			return
		}
		stopMu.Lock()
		defer stopMu.Unlock()

		ctx := c.Request.Context()
		sessionID := c.Param("sessionId")
		session, found := playback.PlaybackSession(ctx, sessionID)
		if !found {
			// 已释放或从未存在：按幂等删除返回 200，不再下发停止指令。
			c.JSON(http.StatusOK, gin.H{
				"sessionId":       sessionID,
				"released":        true,
				"alreadyReleased": true,
				"stopCommand": playbackStopOutcome{
					Reason: "session already released or unknown; stop command not sent",
				},
			})
			return
		}
		// 先尽力下发停止指令，再无条件释放本地会话：下发失败不能把会话挂住。
		stop := sendPlaybackStop(c, devices, conn, downlink, outboxEnabled, session)
		releaseErr := playback.ReleasePlaybackSession(ctx, sessionID)
		fields := gin.H{
			"sessionId":       sessionID,
			"released":        true,
			"alreadyReleased": false,
			"stopCommand":     stop,
		}
		if releaseErr != nil {
			// 会话已从媒体侧摘除，但出口或观众清理报错；如实返回便于排查。
			fields["releaseError"] = releaseErr.Error()
		}
		c.JSON(http.StatusOK, fields)
	})

	api.GET("/device/:phone/recordings/coverage", func(c *gin.Context) {
		if playback == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"err": "media playback is not available on this role"})
			return
		}
		start, err := time.Parse(time.RFC3339, c.Query("start"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "start must be RFC3339"})
			return
		}
		end, err := time.Parse(time.RFC3339, c.Query("end"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "end must be RFC3339"})
			return
		}
		channel, err := strconv.ParseUint(c.DefaultQuery("channel", "0"), 10, 8)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "channel must be 0-255"})
			return
		}
		// 覆盖不完整不是错误：始终 200 + 结构化区间，避免调用方特判 409。
		coverage, err := playback.RecordingCoverage(c.Request.Context(), c.Param("phone"), uint8(channel), start, end)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, coverage)
	})

	api.GET("/device/:phone/recordings/:segmentId/content", func(c *gin.Context) {
		if playback == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"err": "media playback is not available on this role"})
			return
		}
		rangeHeader := c.GetHeader("Range")
		offset, length, hasRange, err := parseByteRange(rangeHeader)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		reader, size, err := playback.OpenRecordingSegment(c.Request.Context(), c.Param("phone"), c.Param("segmentId"), offset, length)
		if err != nil {
			if errors.Is(err, contracts.ErrRecordingNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"err": "recording segment not found"})
				return
			}
			if errors.Is(err, contracts.ErrRangeNotSatisfiable) {
				c.JSON(http.StatusRequestedRangeNotSatisfiable, gin.H{"err": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		defer reader.Close()
		if !hasRange {
			offset = 0
			length = size
		}
		if length <= 0 || offset+length > size {
			length = size - offset
		}
		c.Header("Content-Type", "video/mp2t")
		c.Header("Accept-Ranges", "bytes")
		c.Header("Content-Length", strconv.FormatInt(length, 10))
		status := http.StatusOK
		if hasRange {
			status = http.StatusPartialContent
			c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, offset+length-1, size))
		}
		c.Status(status)
		if c.Request.Method == http.MethodHead {
			return
		}
		_, _ = io.Copy(c.Writer, io.LimitReader(reader, length))
	})
}

// playbackStopOutcome 描述停止指令的下发结果。
//
// notified 只表示直发模式已把报文写入终端连接；outbox 模式下命令仅完成入队，
// 由重投服务异步投递，因此 queued=true 且 notified=false。终端的 0x0001 应答
// 通过 (phone, msgId, serial) 回填命令状态，调用方用 commandId 在
// GET /api/commands/<commandId> 回查是否确认送达。
type playbackStopOutcome struct {
	MessageID string `json:"messageId,omitempty"`
	Notified  bool   `json:"notified"`
	Queued    bool   `json:"queued"`
	Serial    uint16 `json:"serial,omitempty"`
	CommandID string `json:"commandId,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// sendPlaybackStop 按会话类型下发停止指令：实时会话用 0x9102，终端回放用 0x9202。
//
// 控制字：0x9102 的 ControlCmd=0（关闭音视频传输）、CloseMediaType=0（音视频）、
// StreamType=0（该字段仅在切换码流时有效）；0x9202 的 PlayControl=2（结束回放），
// PlaySpeed/DragPosition 不适用，按规范填 0。依据见 msg_9102.go / msg_9202.go
// 与 JT/T 1078-2016 对应表项。
func sendPlaybackStop(
	c *gin.Context,
	devices repository.DeviceRepository,
	conn runtime.ConnectionRegistry,
	downlink contracts.Downlink,
	outboxEnabled bool,
	session contracts.PlaybackSession,
) playbackStopOutcome {
	messageID := "0x9102"
	terminalPlayback := session.Kind == playbackKindTerminal
	if terminalPlayback {
		messageID = "0x9202"
	}
	if downlink == nil {
		return playbackStopOutcome{MessageID: messageID, Reason: "downlink is not configured"}
	}
	ctx := c.Request.Context()
	device, err := devices.GetByPhone(ctx, session.Phone)
	if err != nil {
		return playbackStopOutcome{MessageID: messageID, Reason: "device record not found: " + err.Error()}
	}
	liveSession, err := downlinkSession(ctx, conn, device, outboxEnabled)
	if err != nil {
		return playbackStopOutcome{MessageID: messageID, Reason: err.Error()}
	}
	serial := nextDownlinkSerial(liveSession)
	var msg model.JT808Msg
	if terminalPlayback {
		msg = &model.Msg9202{
			Header:         model.GenMsgHeader(device, 0x9202, serial),
			LogicChannelID: session.Channel,
			PlayControl:    2,
		}
	} else {
		msg = &model.Msg9102{
			Header:         model.GenMsgHeader(device, 0x9102, serial),
			LogicChannelID: session.Channel,
			ControlCmd:     0,
			CloseMediaType: 0,
			StreamType:     0,
		}
	}
	// 幂等键按会话固定：重复 DELETE 命中同一条命令，不会重复下发。
	command, _, err := dispatchDownlinkWithKey(c, downlink, liveSession, msg, "playback-stop:"+session.ID)
	if err != nil {
		return playbackStopOutcome{MessageID: messageID, Reason: err.Error()}
	}
	if command == nil {
		return playbackStopOutcome{MessageID: messageID, Notified: true, Serial: serial}
	}
	return playbackStopOutcome{
		MessageID: messageID,
		Queued:    true,
		Serial:    command.Serial,
		CommandID: command.ID,
	}
}

// parseByteRange 解析单段 Range；只支持 bytes=start-end 与 bytes=start-。
func parseByteRange(header string) (offset, length int64, ok bool, err error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0, 0, false, nil
	}
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false, errors.New("only bytes ranges are supported")
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if strings.Contains(spec, ",") {
		return 0, 0, false, errors.New("multiple ranges are not supported")
	}
	startText, endText, found := strings.Cut(spec, "-")
	if !found {
		return 0, 0, false, errors.New("invalid Range header")
	}
	start, err := strconv.ParseInt(startText, 10, 64)
	if err != nil || start < 0 {
		return 0, 0, false, errors.New("invalid Range start")
	}
	if endText == "" {
		return start, 0, true, nil
	}
	end, err := strconv.ParseInt(endText, 10, 64)
	if err != nil || end < start {
		return 0, 0, false, errors.New("invalid Range end")
	}
	return start, end - start + 1, true, nil
}

func toEndpointJSON(endpoints []contracts.PlaybackEndpoint) []gin.H {
	out := make([]gin.H, 0, len(endpoints))
	for _, endpoint := range endpoints {
		out = append(out, gin.H{"protocol": endpoint.Protocol, "url": endpoint.URL})
	}
	return out
}
