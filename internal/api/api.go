package api

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mitchellh/mapstructure"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/alarmserver"
	protocolhex "github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/httpguard"
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
func Run(ctx context.Context, serv *server.TCPServer, cfg *config.Config, apiOptions config.APIOptions, alarmServ *alarmserver.Server, downlink contracts.Downlink, sender contracts.DownlinkSender, mediaUpload *mediaupload.Store, queryer Queryer, playback contracts.PlaybackLibrary) {
	// web server structure
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	// 顺序为审计 → 限流 → 鉴权：审计能看到被拒绝的请求，限流在鉴权前挡住洪峰。
	router.Use(httpguard.Audit(), httpguard.RateLimit(apiOptions.RatePerSecond, apiOptions.Burst), httpguard.Auth(apiOptions.Token))
	api := router.Group("/api")
	devices := storage.Default().Devices
	geo := storage.Default().Geo
	connRegistry := runtime.Default().Conn
	outboxEnabled := downlink != nil && downlink.Enabled()

	// 0x0801 媒体文件、0x0805 拍摄记录与 0x0802 检索结果查询；文件下载由 mediaUpload 读取共享卷。
	registerMediaRoutes(api, devices, storage.Default().MediaFiles, storage.Default().MediaCaptures, storage.Default().MediaSearches, mediaUpload)

	// 播放会话（实时/终端回放）与平台录像覆盖查询；媒体节点返回播放地址。
	registerPlaybackRoutes(api, devices, connRegistry, downlink, playback)

	// 下行命令投递状态查询；命令下发时返回的 commandId 可在此回查。
	api.GET("/commands/:commandId", func(c *gin.Context) {
		if !outboxEnabled {
			c.JSON(http.StatusNotFound, gin.H{"err": "command outbox is disabled"})
			return
		}
		command, err := downlink.Query(c.Request.Context(), c.Param("commandId"))
		if errors.Is(err, repository.ErrCommandNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"err": "command not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"commandId":     command.ID,
			"phone":         command.Phone,
			"messageId":     fmt.Sprintf("0x%04X", command.MsgID),
			"serial":        command.Serial,
			"status":        command.Status,
			"attempts":      command.Attempts,
			"lastError":     command.LastError,
			"createdAt":     command.CreatedAt,
			"updatedAt":     command.UpdatedAt,
			"lastAttemptAt": command.LastAttemptAt,
			"nextAttemptAt": command.NextAttemptAt,
			"expiresAt":     command.ExpiresAt,
		})
	})

	api.GET("/device", func(c *gin.Context) {
		list, err := devices.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, list)
	})

	api.GET("/device/:phone/reports/:kind", func(c *gin.Context) {
		phone := c.Param("phone")
		if _, err := devices.GetByPhone(c.Request.Context(), phone); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		industry := storage.Default().Industry
		var result any
		switch c.Param("kind") {
		case "driver":
			result = industry.Driver(phone)
		case "can":
			result = industry.CAN(phone)
		case "recorder":
			result = industry.RecordData(phone)
		case "media-event":
			result = industry.MediaEvent(phone)
		case "media-capture":
			result = industry.Capture(phone)
		case "media-search":
			result = industry.MediaSearch(phone)
		case "media-uploads":
			result = industry.MediaUploads(phone)
		case "transparent", "peripheral":
			result = industry.TransparentUp(phone)
		case "upgrade-result":
			upgrade, err := storage.Default().UpgradeResults.Latest(c.Request.Context(), phone)
			if errors.Is(err, repository.ErrUpgradeResultNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"err": "no report received"})
				return
			}
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
				return
			}
			result = upgrade
		default:
			c.JSON(http.StatusBadRequest, gin.H{"err": "kind must be driver, can, recorder, media-event, media-capture, media-search, media-uploads, transparent, peripheral, or upgrade-result"})
			return
		}
		if result == nil {
			c.JSON(http.StatusNotFound, gin.H{"err": "no report received"})
			return
		}
		c.JSON(http.StatusOK, result)
	})

	api.GET("/device/:phone/geo", func(c *gin.Context) {
		phone := c.Param("phone")

		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}

		res := make(map[string]any)
		err = mapstructure.Decode(device, &res)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		gis, err := geo.Latest(c.Request.Context(), phone)
		if err != nil {
			return
		}
		res["gis"] = gis

		c.JSON(http.StatusOK, res)
	})

	api.GET("/device/:phone/params", func(c *gin.Context) {
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		var ids []uint32
		if rawIDs := c.Query("ids"); rawIDs != "" {
			parts := strings.Split(rawIDs, ",")
			if len(parts) > 255 {
				c.JSON(http.StatusBadRequest, gin.H{"err": "at most 255 parameter IDs are allowed"})
				return
			}
			ids = make([]uint32, 0, len(parts))
			for _, raw := range parts {
				id, parseErr := strconv.ParseUint(strings.TrimSpace(raw), 0, 32)
				if parseErr != nil {
					c.JSON(http.StatusBadRequest, gin.H{"err": "ids must be comma-separated decimal or 0x-prefixed uint32 values"})
					return
				}
				ids = append(ids, uint32(id))
			}
		}

		var msg model.JT808Msg
		if len(ids) > 0 {
			msg = &model.Msg8106{Header: model.GenMsgHeader(device, 0x8106, 0), ParamIDs: ids}
		} else {
			msg = &model.Msg8104{Header: model.GenMsgHeader(device, 0x8104, 0)}
		}
		if queryer != nil {
			body, queryErr := queryer.Query(c.Request.Context(), phone, msg, 0x0104, 10*time.Second)
			if queryErr != nil {
				c.JSON(http.StatusGatewayTimeout, gin.H{"err": "terminal parameter query timed out: " + queryErr.Error()})
				return
			}
			decoded, decodeErr := decodeQueryResponse(device, 0x0104, body)
			if decodeErr != nil {
				c.JSON(http.StatusBadGateway, gin.H{"err": decodeErr.Error()})
				return
			}
			response, ok := decoded.(*model.Msg0104)
			if !ok {
				c.JSON(http.StatusBadGateway, gin.H{"err": "unexpected parameter response"})
				return
			}
			c.JSON(http.StatusOK, response.Parameters)
			return
		}

		session, err := localQuerySession(c.Request.Context(), connRegistry, device)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		serial := nextDownlinkSerial(session)
		msg.GetHeader().SerialNumber = serial
		responses := runtime.Default().ParamResponses
		responseCh, cancel, err := responses.Register(phone, serial)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"err": err.Error()})
			return
		}
		defer cancel()

		directSend(c, sender, msg)
		ctx, cancelRequest := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancelRequest()
		select {
		case response := <-responseCh:
			c.JSON(http.StatusOK, response.Parameters)
		case <-ctx.Done():
			if c.Request.Context().Err() != nil {
				return
			}
			c.JSON(http.StatusGatewayTimeout, gin.H{"err": "terminal parameter query timed out"})
		}
	})

	api.GET("/device/:phone/properties", func(c *gin.Context) {
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		msg := &model.Msg8107{Header: model.GenMsgHeader(device, 0x8107, 0)}
		if queryer != nil {
			body, queryErr := queryer.Query(c.Request.Context(), phone, msg, 0x0107, 10*time.Second)
			if queryErr != nil {
				c.JSON(http.StatusGatewayTimeout, gin.H{"err": "terminal property query timed out: " + queryErr.Error()})
				return
			}
			decoded, decodeErr := decodeQueryResponse(device, 0x0107, body)
			if decodeErr != nil {
				c.JSON(http.StatusBadGateway, gin.H{"err": decodeErr.Error()})
				return
			}
			c.JSON(http.StatusOK, decoded)
			return
		}
		session, err := localQuerySession(c.Request.Context(), connRegistry, device)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		msg.Header.SerialNumber = nextDownlinkSerial(session)
		responses := runtime.Default().PropertyResponses
		responseCh, cancel, err := responses.Register(phone)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"err": err.Error()})
			return
		}
		defer cancel()
		directSend(c, sender, msg)
		ctx, cancelRequest := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancelRequest()
		select {
		case response := <-responseCh:
			c.JSON(http.StatusOK, response)
		case <-ctx.Done():
			if c.Request.Context().Err() != nil {
				return
			}
			c.JSON(http.StatusGatewayTimeout, gin.H{"err": "terminal property query timed out"})
		}
	})

	api.GET("/device/:phone/location", func(c *gin.Context) {
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		msg := &model.Msg8201{Header: model.GenMsgHeader(device, 0x8201, 0)}
		if queryer != nil {
			body, queryErr := queryer.Query(c.Request.Context(), phone, msg, 0x0201, 10*time.Second)
			if queryErr != nil {
				c.JSON(http.StatusGatewayTimeout, gin.H{"err": "terminal location query timed out: " + queryErr.Error()})
				return
			}
			decoded, decodeErr := decodeQueryResponse(device, 0x0201, body)
			if decodeErr != nil {
				c.JSON(http.StatusBadGateway, gin.H{"err": decodeErr.Error()})
				return
			}
			response, ok := decoded.(*model.Msg0201)
			if !ok {
				c.JSON(http.StatusBadGateway, gin.H{"err": "unexpected location response"})
				return
			}
			c.JSON(http.StatusOK, response.Location)
			return
		}
		session, err := localQuerySession(c.Request.Context(), connRegistry, device)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		serial := nextDownlinkSerial(session)
		msg.Header.SerialNumber = serial
		responses := runtime.Default().LocationResponses
		responseCh, cancel, err := responses.Register(phone, serial)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"err": err.Error()})
			return
		}
		defer cancel()
		directSend(c, sender, msg)
		ctx, cancelRequest := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancelRequest()
		select {
		case response := <-responseCh:
			c.JSON(http.StatusOK, response.Location)
		case <-ctx.Done():
			if c.Request.Context().Err() != nil {
				return
			}
			c.JSON(http.StatusGatewayTimeout, gin.H{"err": "terminal location query timed out"})
		}
	})

	api.POST("/device/:phone/control", func(c *gin.Context) {
		var request struct {
			CommandWord  uint8                    `json:"commandWord" binding:"required"`
			CommandParam string                   `json:"commandParam"`
			Parameters   []model.CommandParameter `json:"parameters"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		if request.CommandWord != 1 && request.CommandWord != 2 && request.CommandWord != 4 && request.CommandWord != 5 {
			c.JSON(http.StatusBadRequest, gin.H{"err": "supported commandWord values are 1 (upgrade), 2 (connect server), 4 (reset), and 5 (factory reset)"})
			return
		}
		typed := len(request.Parameters) > 0
		if typed && request.CommandParam != "" {
			c.JSON(http.StatusBadRequest, gin.H{"err": "commandParam and parameters are mutually exclusive"})
			return
		}
		if typed && request.CommandWord != 1 && request.CommandWord != 2 {
			c.JSON(http.StatusBadRequest, gin.H{"err": "parameters are only valid for commandWord 1 and 2"})
			return
		}
		if !typed && request.CommandWord != 2 && request.CommandParam != "" {
			c.JSON(http.StatusBadRequest, gin.H{"err": "commandParam is only valid for commandWord 2"})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		if !typed && len(request.CommandParam) > 256 {
			c.JSON(http.StatusBadRequest, gin.H{"err": "commandParam exceeds 256 UTF-8 bytes"})
			return
		}
		if typed {
			seen := make(map[uint8]struct{}, len(request.Parameters))
			for _, param := range request.Parameters {
				if int(param.Length) != len(param.Value) {
					c.JSON(http.StatusBadRequest, gin.H{"err": fmt.Sprintf("parameter %d length %d does not match value length %d", param.ID, param.Length, len(param.Value))})
					return
				}
				if len(param.Value) > 255 {
					c.JSON(http.StatusBadRequest, gin.H{"err": fmt.Sprintf("parameter %d exceeds 255 bytes", param.ID)})
					return
				}
				if _, exists := seen[param.ID]; exists {
					c.JSON(http.StatusBadRequest, gin.H{"err": fmt.Sprintf("duplicate parameter id %d", param.ID)})
					return
				}
				seen[param.ID] = struct{}{}
			}
		}
		msg := &model.Msg8105{Header: model.GenMsgHeader(device, 0x8105, nextDownlinkSerial(session)), CommandWord: request.CommandWord, CommandParam: request.CommandParam, Parameters: request.Parameters}
		command, created, err := dispatchDownlink(c, downlink, session, msg)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8105", "commandWord": request.CommandWord})
	})

	api.POST("/device/:phone/regions/delete", func(c *gin.Context) {
		var request struct {
			MessageID uint16   `json:"messageId" binding:"required"`
			IDs       []uint32 `json:"ids"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		if request.MessageID != 0x8601 && request.MessageID != 0x8603 && request.MessageID != 0x8605 && request.MessageID != 0x8607 {
			c.JSON(http.StatusBadRequest, gin.H{"err": "messageId must be 0x8601, 0x8603, 0x8605, or 0x8607"})
			return
		}
		if len(request.IDs) > 125 {
			c.JSON(http.StatusBadRequest, gin.H{"err": "at most 125 IDs are allowed per message"})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		msg := &model.Msg860Delete{Header: model.GenMsgHeader(device, request.MessageID, nextDownlinkSerial(session)), IDs: request.IDs}
		command, created, err := dispatchDownlink(c, downlink, session, msg)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": request.MessageID, "count": len(request.IDs)})
	})

	api.POST("/device/:phone/regions/set", func(c *gin.Context) {
		var request struct {
			MessageID   uint16                `json:"messageId" binding:"required"`
			SettingAttr uint8                 `json:"settingAttr"`
			Circles     []model.CircleArea    `json:"circles"`
			Rectangles  []model.RectangleArea `json:"rectangles"`
			Polygons    []model.PolygonArea   `json:"polygons"`
			Routes      []model.Route         `json:"routes"`
			BodyHex     string                `json:"bodyHex"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		if request.MessageID != 0x8600 && request.MessageID != 0x8602 && request.MessageID != 0x8604 && request.MessageID != 0x8606 {
			c.JSON(http.StatusBadRequest, gin.H{"err": "messageId must be 0x8600, 0x8602, 0x8604, or 0x8606"})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}

		var msg *model.MsgRegionRouteSet
		var count int
		if request.BodyHex != "" {
			body, err := hex.DecodeString(request.BodyHex)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"err": "bodyHex must be an even-length hexadecimal string"})
				return
			}
			// Round-trip through the typed decoder so malformed bodies are rejected.
			msg = &model.MsgRegionRouteSet{Header: model.GenMsgHeader(device, request.MessageID, nextDownlinkSerial(session))}
			if err := msg.Decode(&model.PacketData{Header: msg.Header, Body: body}); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
				return
			}
			count = msg.ItemCount()
			msg.Header = model.GenMsgHeader(device, request.MessageID, nextDownlinkSerial(session))
		} else {
			msg = &model.MsgRegionRouteSet{
				Header:      model.GenMsgHeader(device, request.MessageID, nextDownlinkSerial(session)),
				SettingAttr: model.AreaSettingAttr(request.SettingAttr),
				Circles:     request.Circles,
				Rectangles:  request.Rectangles,
				Polygons:    request.Polygons,
				Routes:      request.Routes,
			}
			count = msg.ItemCount()
		}
		if count == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"err": "at least one region/route item is required"})
			return
		}
		command, created, err := dispatchDownlink(c, downlink, session, msg)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": request.MessageID, "count": count})
	})

	api.POST("/device/:phone/regions/query", func(c *gin.Context) {
		var request struct {
			QueryType uint8    `json:"queryType" binding:"required,min=1,max=4"`
			IDs       []uint32 `json:"ids"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		if len(request.IDs) > 125 {
			c.JSON(http.StatusBadRequest, gin.H{"err": "at most 125 IDs are allowed per query"})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		if queryer != nil {
			msg := &model.Msg8608{Header: model.GenMsgHeader(device, 0x8608, 0), QueryType: request.QueryType, IDs: request.IDs}
			body, queryErr := queryer.Query(c.Request.Context(), phone, msg, 0x0608, 10*time.Second)
			if queryErr != nil {
				c.JSON(http.StatusGatewayTimeout, gin.H{"err": "region/route query timed out: " + queryErr.Error()})
				return
			}
			decoded, decodeErr := decodeQueryResponse(device, 0x0608, body)
			if decodeErr != nil {
				c.JSON(http.StatusBadGateway, gin.H{"err": decodeErr.Error()})
				return
			}
			c.JSON(http.StatusOK, decoded)
			return
		}
		session, err := localQuerySession(c.Request.Context(), connRegistry, device)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		broker := runtime.Default().RegionResponses
		response, cancel, err := broker.Register(phone, request.QueryType)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"err": err.Error()})
			return
		}
		defer cancel()
		directSend(c, sender, &model.Msg8608{Header: model.GenMsgHeader(device, 0x8608, nextDownlinkSerial(session)), QueryType: request.QueryType, IDs: request.IDs})
		ctx, cancelRequest := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancelRequest()
		select {
		case result := <-response:
			c.JSON(http.StatusOK, result)
		case <-ctx.Done():
			if c.Request.Context().Err() != nil {
				return
			}
			c.JSON(http.StatusGatewayTimeout, gin.H{"err": "region/route query timed out"})
		}
	})

	api.POST("/device/:phone/driver/query", func(c *gin.Context) {
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		directSend(c, sender, &model.Msg8702{Header: model.GenMsgHeader(device, 0x8702, nextDownlinkSerial(session))})
		c.JSON(http.StatusAccepted, gin.H{"messageId": "0x8702"})
	})
	api.POST("/device/:phone/recorder/command", func(c *gin.Context) {
		var request struct {
			MessageID uint16 `json:"messageId" binding:"required"`
			Command   uint8  `json:"command"`
			DataHex   string `json:"dataHex"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		if request.MessageID != 0x8700 && request.MessageID != 0x8701 {
			c.JSON(http.StatusBadRequest, gin.H{"err": "messageId must be 0x8700 or 0x8701"})
			return
		}
		data, err := hex.DecodeString(request.DataHex)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "dataHex must be an even-length hexadecimal string"})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		msg := &model.MsgRecordCommand{Header: model.GenMsgHeader(device, request.MessageID, nextDownlinkSerial(session)), Command: request.Command, Data: data}
		command, created, err := dispatchDownlink(c, downlink, session, msg)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": request.MessageID, "command": request.Command})
	})
	api.POST("/device/:phone/media/capture", func(c *gin.Context) {
		var request model.Msg8801
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8801, nextDownlinkSerial(session))
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid camera command fields"})
			return
		}
		command, created, err := dispatchDownlink(c, downlink, session, &request)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8801"})
	})
	api.POST("/device/:phone/media/search", func(c *gin.Context) {
		var request model.Msg8802
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8802, nextDownlinkSerial(session))
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid media search fields"})
			return
		}
		directSend(c, sender, &request)
		c.JSON(http.StatusAccepted, gin.H{"messageId": "0x8802"})
	})
	api.POST("/device/:phone/media/resources", func(c *gin.Context) {
		var request struct {
			LogicChannelID uint8  `json:"logicChannelId"`
			StartTime      string `json:"startTime"`
			EndTime        string `json:"endTime"`
			AlarmSign      uint32 `json:"alarmSign"`
			AlarmSignExt   uint32 `json:"alarmSignExt"`
			MediaType      uint8  `json:"mediaType"`
			StreamType     uint8  `json:"streamType"`
			StorageType    uint8  `json:"storageType"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		msg := model.Msg9205{}
		msg.LogicChannelID = request.LogicChannelID
		msg.AlarmSign = request.AlarmSign
		msg.AlarmSignExt = request.AlarmSignExt
		msg.MediaType = request.MediaType
		msg.StreamType = request.StreamType
		msg.StorageType = request.StorageType
		if request.StartTime != "" {
			t, err := time.ParseInLocation("2006-01-02 15:04:05", request.StartTime, protocolhex.ProtocolTimeZone)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"err": "startTime must be '2006-01-02 15:04:05' format"})
				return
			}
			msg.StartTime = &t
		}
		if request.EndTime != "" {
			t, err := time.ParseInLocation("2006-01-02 15:04:05", request.EndTime, protocolhex.ProtocolTimeZone)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"err": "endTime must be '2006-01-02 15:04:05' format"})
				return
			}
			msg.EndTime = &t
		}
		msg.Header = model.GenMsgHeader(device, 0x9205, nextDownlinkSerial(session))
		if _, err := msg.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid resource query fields"})
			return
		}
		directSend(c, sender, &msg)
		c.JSON(http.StatusAccepted, gin.H{"messageId": "0x9205"})
	})
	api.POST("/device/:phone/media/command", func(c *gin.Context) {
		var request struct {
			MessageID uint16 `json:"messageId" binding:"required"`
			BodyHex   string `json:"bodyHex"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		if request.MessageID != 0x8803 && request.MessageID != 0x8804 && request.MessageID != 0x8805 {
			c.JSON(http.StatusBadRequest, gin.H{"err": "messageId must be 0x8803, 0x8804, or 0x8805"})
			return
		}
		body, err := hex.DecodeString(request.BodyHex)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "bodyHex must be an even-length hexadecimal string"})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		header := model.GenMsgHeader(device, request.MessageID, nextDownlinkSerial(session))
		packet := &model.PacketData{Header: header, Body: body}
		var msg model.JT808Msg
		switch request.MessageID {
		case 0x8803:
			msg = &model.Msg8803{}
		case 0x8804:
			msg = &model.Msg8804{}
		case 0x8805:
			msg = &model.Msg8805{}
		}
		if err := msg.Decode(packet); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "bodyHex does not match the selected JT808 media command format"})
			return
		}
		command, created, err := dispatchDownlink(c, downlink, session, msg)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": fmt.Sprintf("0x%04X", request.MessageID)})
	})

	api.POST("/device/:phone/text/issue", func(c *gin.Context) {
		var request model.Msg8300
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8300, nextDownlinkSerial(session))
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid text issue fields"})
			return
		}
		command, created, err := dispatchDownlink(c, downlink, session, &request)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8300"})
	})
	api.POST("/device/:phone/events/set", func(c *gin.Context) {
		var request model.Msg8301
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.ItemCount = uint8(len(request.Items))
		request.Header = model.GenMsgHeader(device, 0x8301, nextDownlinkSerial(session))
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid event set fields"})
			return
		}
		command, created, err := dispatchDownlink(c, downlink, session, &request)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8301"})
	})

	api.POST("/device/:phone/vehicle/control", func(c *gin.Context) {
		var request model.Msg8500
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8500, nextDownlinkSerial(session))
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid vehicle control fields"})
			return
		}
		command, created, err := dispatchDownlink(c, downlink, session, &request)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8500"})
	})

	api.POST("/device/:phone/question/issue", func(c *gin.Context) {
		var request model.Msg8302
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8302, nextDownlinkSerial(session))
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid question issue fields"})
			return
		}
		command, created, err := dispatchDownlink(c, downlink, session, &request)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8302"})
	})

	api.POST("/device/:phone/info/menu/set", func(c *gin.Context) {
		var request model.Msg8303
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8303, nextDownlinkSerial(session))
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid info menu set fields"})
			return
		}
		command, created, err := dispatchDownlink(c, downlink, session, &request)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8303"})
	})

	api.POST("/device/:phone/info/service", func(c *gin.Context) {
		var request model.Msg8304
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8304, nextDownlinkSerial(session))
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid info service fields"})
			return
		}
		command, created, err := dispatchDownlink(c, downlink, session, &request)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8304"})
	})

	api.POST("/device/:phone/call/callback", func(c *gin.Context) {
		var request model.Msg8400
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8400, nextDownlinkSerial(session))
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid call callback fields"})
			return
		}
		command, created, err := dispatchDownlink(c, downlink, session, &request)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8400"})
	})

	api.POST("/device/:phone/phonebook/set", func(c *gin.Context) {
		var request model.Msg8401
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8401, nextDownlinkSerial(session))
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid phonebook set fields"})
			return
		}
		command, created, err := dispatchDownlink(c, downlink, session, &request)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8401"})
	})

	api.POST("/device/:phone/terminal/upgrade", func(c *gin.Context) {
		var request model.Msg8108
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8108, nextDownlinkSerial(session))
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid terminal upgrade fields"})
			return
		}
		command, created, err := dispatchDownlink(c, downlink, session, &request)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8108"})
	})

	api.POST("/device/:phone/transparent/down", func(c *gin.Context) {
		var request model.Msg8900
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8900, nextDownlinkSerial(session))
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid transparent downlink fields"})
			return
		}
		command, created, err := dispatchDownlink(c, downlink, session, &request)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8900"})
	})

	api.POST("/device/:phone/alarms/confirm", func(c *gin.Context) {
		var request struct {
			AlarmSerial uint16 `json:"alarmSerial"`
			AlarmType   uint32 `json:"alarmType"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		msg := &model.Msg8203{Header: model.GenMsgHeader(device, 0x8203, nextDownlinkSerial(session)), AlarmSerial: request.AlarmSerial, AlarmType: request.AlarmType}
		if err := storage.Default().Alarms.RegisterConfirmation(c.Request.Context(), phone, msg.Header.SerialNumber, request.AlarmSerial, request.AlarmType); err != nil {
			log.Error().Err(err).Str("phone", phone).Msg("register alarm confirmation")
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		directSend(c, sender, msg)
		c.JSON(http.StatusAccepted, gin.H{"messageId": "0x8203", "alarmSerial": request.AlarmSerial, "alarmType": request.AlarmType})
	})

	api.GET("/device/:phone/alarms", func(c *gin.Context) {
		phone := c.Param("phone")
		if _, err := devices.GetByPhone(c.Request.Context(), phone); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		alarms, err := storage.Default().Alarms.List(c.Request.Context(), phone)
		if err != nil {
			log.Error().Err(err).Str("phone", phone).Msg("list alarms")
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, alarms)
	})

	api.PUT("/device/:phone/params", func(c *gin.Context) {
		phone := c.Param("phone")
		params := model.DeviceParams{}
		if err := c.ShouldBind(&params); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		header := model.GenMsgHeader(device, 0x8103, nextDownlinkSerial(session))
		msg := model.Msg8103{
			Header:     header,
			Parameters: &params,
		}
		command, created, err := dispatchDownlink(c, downlink, session, &msg)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8103"})
	})

	// 苏标主动安全附件：下发 0x9208 报警附件上传指令。
	api.POST("/device/:phone/attachments/upload", func(c *gin.Context) {
		if alarmServ == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"err": "alarm server is not enabled"})
			return
		}
		var request struct {
			IP           string `json:"ip" binding:"required"`
			TCPPort      uint16 `json:"tcpPort"`
			UDPPort      uint16 `json:"udpPort"`
			AlarmSignHex string `json:"alarmSignHex" binding:"required"`
			AlarmNo      string `json:"alarmNo" binding:"required"`
			ReservedHex  string `json:"reservedHex"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		ip := strings.TrimSpace(request.IP)
		if ip == "" || len(ip) > 255 {
			c.JSON(http.StatusBadRequest, gin.H{"err": "ip must contain 1 to 255 bytes"})
			return
		}
		alarmSign, err := hex.DecodeString(strings.TrimSpace(request.AlarmSignHex))
		if err != nil || len(alarmSign) != 16 {
			c.JSON(http.StatusBadRequest, gin.H{"err": "alarmSignHex must be 16 decoded bytes"})
			return
		}
		if request.AlarmNo == "" || len([]byte(request.AlarmNo)) > 32 {
			c.JSON(http.StatusBadRequest, gin.H{"err": "alarmNo must contain 1 to 32 UTF-8 bytes"})
			return
		}
		var reserved []byte
		if request.ReservedHex != "" {
			reserved, err = hex.DecodeString(strings.TrimSpace(request.ReservedHex))
			if err != nil || len(reserved) > 16 {
				c.JSON(http.StatusBadRequest, gin.H{"err": "reservedHex must decode to at most 16 bytes"})
				return
			}
		}
		// 未显式指定端口时回落到附件服务器的实际监听端口，
		// 保证 0x9208 通告的地址不会指向没有监听的协议端口。
		resolvePort := func(addr string) uint16 {
			if addr == "" {
				return 0
			}
			_, portText, splitErr := net.SplitHostPort(addr)
			parsed, parseErr := strconv.Atoi(portText)
			if splitErr != nil || parseErr != nil || parsed < 1 || parsed > 65535 {
				return 0
			}
			return uint16(parsed)
		}
		tcpPort := request.TCPPort
		if tcpPort == 0 {
			tcpPort = resolvePort(alarmServ.Addr())
		}
		udpPort := request.UDPPort
		if udpPort == 0 {
			udpPort = resolvePort(alarmServ.UDPAddr())
		}
		if tcpPort == 0 && udpPort == 0 {
			c.JSON(http.StatusServiceUnavailable, gin.H{"err": "alarm server has no tcp or udp listener"})
			return
		}

		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := downlinkSession(c.Request.Context(), connRegistry, device, outboxEnabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		msg := &model.Msg9208{
			Header:    model.GenMsgHeader(device, 0x9208, nextDownlinkSerial(session)),
			IP:        ip,
			TCPPort:   tcpPort,
			UDPPort:   udpPort,
			AlarmSign: alarmSign,
			AlarmNo:   request.AlarmNo,
			Reserved:  reserved,
		}
		command, created, err := dispatchDownlink(c, downlink, session, msg)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		writeCommandAccepted(c, command, created, gin.H{"messageId": "0x9208", "tcpPort": tcpPort, "udpPort": udpPort})
	})

	// 苏标主动安全附件：查询已完成附件的元数据。
	api.GET("/device/:phone/attachments", func(c *gin.Context) {
		if alarmServ == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"err": "alarm server is not enabled"})
			return
		}
		phone := c.Param("phone")
		if _, err := devices.GetByPhone(c.Request.Context(), phone); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, alarmServ.Assembler().Select(phone))
	})

	api.GET("/device/:phone/attachments/:fileName", func(c *gin.Context) {
		if alarmServ == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"err": "alarm server is not enabled"})
			return
		}
		phone := c.Param("phone")
		if _, err := devices.GetByPhone(c.Request.Context(), phone); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		meta, err := alarmServ.Assembler().FindMetadata(phone, c.Param("fileName"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, meta)
	})

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
func decodeQueryResponse(device *model.Device, msgID uint16, body []byte) (model.JT808Msg, error) {
	header := model.GenMsgHeader(device, msgID, 0)
	return protocol.DecodeMessageBody(msgID, header, body)
}

// lookupSessionByPhone 按终端手机号在运行时会话注册表中定位当前活连接。
// SQL 后端不再持久化运行时字段，Device.SessionID 读出来是零值；
// 下行路由因此必须走手机号索引，不能依赖该字段。
func lookupSessionByPhone(ctx context.Context, conn runtime.ConnectionRegistry, device *model.Device) (*model.Session, error) {
	return conn.GetByPhone(ctx, device.Phone)
}

// localQuerySession 供依赖进程内应答 broker 的查询分支使用。
//
// 这些分支只在 queryer 为空（standalone）时进入：HTTP 请求同步等待本进程
// 收到的应答，必须有本地会话才能发送并注册等待方。
func localQuerySession(ctx context.Context, conn runtime.ConnectionRegistry, device *model.Device) (*model.Session, error) {
	return lookupSessionByPhone(ctx, conn, device)
}

// downlinkSession 在 outbox 启用时返回 nil：流水号与投递由管控侧完成，
// control-only 没有本地会话也能下发；off 模式仍要求本地会话，保持原有 400 行为。
func downlinkSession(ctx context.Context, conn runtime.ConnectionRegistry, device *model.Device, outboxEnabled bool) (*model.Session, error) {
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
	if downlink == nil {
		return nil, false, errors.New("command outbox is not configured")
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if len(idempotencyKey) > 128 {
		return nil, false, errors.New("Idempotency-Key must not exceed 128 characters")
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
