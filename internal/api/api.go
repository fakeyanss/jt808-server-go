package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mitchellh/mapstructure"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/alarmserver"
	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
	"github.com/fakeyanss/jt808-server-go/internal/server"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

func Run(serv *server.TCPServer, cfg *config.Config, alarmServ *alarmserver.Server) {
	// web server structure
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	api := router.Group("/api")
	devices := storage.Default().Devices
	geo := storage.Default().Geo
	connRegistry := runtime.Default().Conn
	commands := storage.Default().Commands
	// 入队时写入首次可重投时刻与过期时刻；配置非法时回退默认值
	// （启动阶段已由 main 校验，这里仅做兜底）。
	outboxOptions, outboxErr := cfg.ParseCommandOutbox()
	if outboxErr != nil {
		log.Error().Err(outboxErr).Msg("Invalid commandOutbox configuration, using defaults")
		outboxOptions = config.DefaultCommandOutboxOptions()
	}

	// 下行命令投递状态查询；命令下发时返回的 commandId 可在此回查。
	api.GET("/commands/:commandId", func(c *gin.Context) {
		command, err := commands.Get(c.Request.Context(), c.Param("commandId"))
		if errors.Is(err, repository.ErrCommandNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"err": "command not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"commandId":     command.CommandID,
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
		default:
			c.JSON(http.StatusBadRequest, gin.H{"err": "kind must be driver, can, recorder, media-event, media-capture, media-search, media-uploads, transparent, or peripheral"})
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		serial := session.GetNextSerialNum()
		responses := runtime.Default().ParamResponses
		responseCh, cancel, err := responses.Register(phone, serial)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"err": err.Error()})
			return
		}
		defer cancel()

		var msg model.JT808Msg
		if rawIDs := c.Query("ids"); rawIDs != "" {
			parts := strings.Split(rawIDs, ",")
			if len(parts) > 255 {
				c.JSON(http.StatusBadRequest, gin.H{"err": "at most 255 parameter IDs are allowed"})
				return
			}
			ids := make([]uint32, 0, len(parts))
			for _, raw := range parts {
				id, parseErr := strconv.ParseUint(strings.TrimSpace(raw), 0, 32)
				if parseErr != nil {
					c.JSON(http.StatusBadRequest, gin.H{"err": "ids must be comma-separated decimal or 0x-prefixed uint32 values"})
					return
				}
				ids = append(ids, uint32(id))
			}
			msg = &model.Msg8106{Header: model.GenMsgHeader(device, 0x8106, serial), ParamIDs: ids}
		} else {
			msg = &model.Msg8104{Header: model.GenMsgHeader(device, 0x8104, serial)}
		}
		serv.Send(session.ID, msg)
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		responses := runtime.Default().PropertyResponses
		responseCh, cancel, err := responses.Register(phone)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"err": err.Error()})
			return
		}
		defer cancel()
		serv.Send(session.ID, &model.Msg8107{Header: model.GenMsgHeader(device, 0x8107, session.GetNextSerialNum())})
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		serial := session.GetNextSerialNum()
		responses := runtime.Default().LocationResponses
		responseCh, cancel, err := responses.Register(phone, serial)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"err": err.Error()})
			return
		}
		defer cancel()
		serv.Send(session.ID, &model.Msg8201{Header: model.GenMsgHeader(device, 0x8201, serial)})
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
			CommandWord  uint8  `json:"commandWord" binding:"required"`
			CommandParam string `json:"commandParam"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		if request.CommandWord != 2 && request.CommandWord != 4 && request.CommandWord != 5 {
			c.JSON(http.StatusBadRequest, gin.H{"err": "supported commandWord values are 2 (connect server), 4 (reset), and 5 (factory reset)"})
			return
		}
		if request.CommandWord != 2 && request.CommandParam != "" {
			c.JSON(http.StatusBadRequest, gin.H{"err": "commandParam is only valid for commandWord 2"})
			return
		}
		phone := c.Param("phone")
		device, err := devices.GetByPhone(c.Request.Context(), phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		if len(request.CommandParam) > 256 {
			c.JSON(http.StatusBadRequest, gin.H{"err": "commandParam exceeds 256 UTF-8 bytes"})
			return
		}
		msg := &model.Msg8105{Header: model.GenMsgHeader(device, 0x8105, session.GetNextSerialNum()), CommandWord: request.CommandWord, CommandParam: request.CommandParam}
		command, created, err := enqueueDownlinkCommand(c, commands, serv, session, msg, outboxOptions)
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		msg := &model.Msg860Delete{Header: model.GenMsgHeader(device, request.MessageID, session.GetNextSerialNum()), IDs: request.IDs}
		serv.Send(session.ID, msg)
		c.JSON(http.StatusAccepted, gin.H{"messageId": request.MessageID, "count": len(request.IDs)})
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
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
			msg = &model.MsgRegionRouteSet{Header: model.GenMsgHeader(device, request.MessageID, session.GetNextSerialNum())}
			if err := msg.Decode(&model.PacketData{Header: msg.Header, Body: body}); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
				return
			}
			count = msg.ItemCount()
			msg.Header = model.GenMsgHeader(device, request.MessageID, session.GetNextSerialNum())
		} else {
			msg = &model.MsgRegionRouteSet{
				Header:      model.GenMsgHeader(device, request.MessageID, session.GetNextSerialNum()),
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
		serv.Send(session.ID, msg)
		c.JSON(http.StatusAccepted, gin.H{"messageId": request.MessageID, "count": count})
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
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
		serv.Send(session.ID, &model.Msg8608{Header: model.GenMsgHeader(device, 0x8608, session.GetNextSerialNum()), QueryType: request.QueryType, IDs: request.IDs})
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		serv.Send(session.ID, &model.Msg8702{Header: model.GenMsgHeader(device, 0x8702, session.GetNextSerialNum())})
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		msg := &model.MsgRecordCommand{Header: model.GenMsgHeader(device, request.MessageID, session.GetNextSerialNum()), Command: request.Command, Data: data}
		command, created, err := enqueueDownlinkCommand(c, commands, serv, session, msg, outboxOptions)
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8801, session.GetNextSerialNum())
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid camera command fields"})
			return
		}
		serv.Send(session.ID, &request)
		c.JSON(http.StatusAccepted, gin.H{"messageId": "0x8801"})
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8802, session.GetNextSerialNum())
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid media search fields"})
			return
		}
		serv.Send(session.ID, &request)
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
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
			t, err := time.Parse("2006-01-02 15:04:05", request.StartTime)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"err": "startTime must be '2006-01-02 15:04:05' format"})
				return
			}
			msg.StartTime = &t
		}
		if request.EndTime != "" {
			t, err := time.Parse("2006-01-02 15:04:05", request.EndTime)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"err": "endTime must be '2006-01-02 15:04:05' format"})
				return
			}
			msg.EndTime = &t
		}
		msg.Header = model.GenMsgHeader(device, 0x9205, session.GetNextSerialNum())
		if _, err := msg.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid resource query fields"})
			return
		}
		serv.Send(session.ID, &msg)
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		header := model.GenMsgHeader(device, request.MessageID, session.GetNextSerialNum())
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
		serv.Send(session.ID, msg)
		c.JSON(http.StatusAccepted, gin.H{"messageId": request.MessageID})
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8300, session.GetNextSerialNum())
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid text issue fields"})
			return
		}
		serv.Send(session.ID, &request)
		c.JSON(http.StatusAccepted, gin.H{"messageId": "0x8300"})
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.ItemCount = uint8(len(request.Items))
		request.Header = model.GenMsgHeader(device, 0x8301, session.GetNextSerialNum())
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid event set fields"})
			return
		}
		serv.Send(session.ID, &request)
		c.JSON(http.StatusAccepted, gin.H{"messageId": "0x8301"})
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8500, session.GetNextSerialNum())
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid vehicle control fields"})
			return
		}
		serv.Send(session.ID, &request)
		c.JSON(http.StatusAccepted, gin.H{"messageId": "0x8500"})
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8302, session.GetNextSerialNum())
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid question issue fields"})
			return
		}
		serv.Send(session.ID, &request)
		c.JSON(http.StatusAccepted, gin.H{"messageId": "0x8302"})
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8303, session.GetNextSerialNum())
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid info menu set fields"})
			return
		}
		serv.Send(session.ID, &request)
		c.JSON(http.StatusAccepted, gin.H{"messageId": "0x8303"})
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8304, session.GetNextSerialNum())
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid info service fields"})
			return
		}
		serv.Send(session.ID, &request)
		c.JSON(http.StatusAccepted, gin.H{"messageId": "0x8304"})
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8400, session.GetNextSerialNum())
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid call callback fields"})
			return
		}
		command, created, err := enqueueDownlinkCommand(c, commands, serv, session, &request, outboxOptions)
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8401, session.GetNextSerialNum())
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid phonebook set fields"})
			return
		}
		command, created, err := enqueueDownlinkCommand(c, commands, serv, session, &request, outboxOptions)
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8108, session.GetNextSerialNum())
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid terminal upgrade fields"})
			return
		}
		command, created, err := enqueueDownlinkCommand(c, commands, serv, session, &request, outboxOptions)
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		request.Header = model.GenMsgHeader(device, 0x8900, session.GetNextSerialNum())
		if _, err := request.Encode(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": "invalid transparent downlink fields"})
			return
		}
		command, created, err := enqueueDownlinkCommand(c, commands, serv, session, &request, outboxOptions)
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		msg := &model.Msg8203{Header: model.GenMsgHeader(device, 0x8203, session.GetNextSerialNum()), AlarmSerial: request.AlarmSerial, AlarmType: request.AlarmType}
		if err := storage.Default().Alarms.RegisterConfirmation(c.Request.Context(), phone, msg.Header.SerialNumber, request.AlarmSerial, request.AlarmType); err != nil {
			log.Error().Err(err).Str("phone", phone).Msg("register alarm confirmation")
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		serv.Send(session.ID, msg)
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		header := model.GenMsgHeader(device, 0x8103, session.GetNextSerialNum())
		msg := model.Msg8103{
			Header:     header,
			Parameters: &params,
		}
		serv.Send(session.ID, &msg)
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
		session, err := connRegistry.Get(c.Request.Context(), device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		msg := &model.Msg9208{
			Header:    model.GenMsgHeader(device, 0x9208, session.GetNextSerialNum()),
			IP:        ip,
			TCPPort:   tcpPort,
			UDPPort:   udpPort,
			AlarmSign: alarmSign,
			AlarmNo:   request.AlarmNo,
			Reserved:  reserved,
		}
		serv.Send(session.ID, msg)
		c.JSON(http.StatusAccepted, gin.H{"messageId": "0x9208", "tcpPort": tcpPort, "udpPort": udpPort})
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
	err := router.Run(httpAddr)
	if err != nil {
		log.Error().Err(err).Str("addr", httpAddr).Msg("Fail to run gin router")
		os.Exit(1)
	}
}

// enqueueDownlinkCommand 把下行命令写入 outbox 后再发送。
// Idempotency-Key 请求头重复时直接返回已有记录（duplicate=true），不会二次下发。
// 返回的 created=false 表示命中了幂等键。
func enqueueDownlinkCommand(
	c *gin.Context,
	commands repository.CommandRepository,
	serv *server.TCPServer,
	session *model.Session,
	msg model.JT808Msg,
	outbox config.CommandOutboxOptions,
) (*repository.DownlinkCommand, bool, error) {
	header := msg.GetHeader()
	if header == nil {
		return nil, false, errors.New("downlink message header is required")
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if len(idempotencyKey) > 128 {
		return nil, false, errors.New("Idempotency-Key must not exceed 128 characters")
	}
	commandID, err := newCommandID()
	if err != nil {
		return nil, false, err
	}
	if idempotencyKey == "" {
		idempotencyKey = commandID
	}

	serial := session.GetNextSerialNum()
	header.SerialNumber = serial
	encoded, err := msg.Encode()
	if err != nil {
		return nil, false, fmt.Errorf("encode downlink message: %w", err)
	}
	headerBytes, err := header.Encode()
	if err != nil {
		return nil, false, fmt.Errorf("encode downlink header: %w", err)
	}
	if len(headerBytes) >= len(encoded) {
		return nil, false, errors.New("encoded downlink message has no body")
	}

	now := time.Now()
	command, created, err := commands.Enqueue(c.Request.Context(), &repository.DownlinkCommand{
		CommandID:      commandID,
		IdempotencyKey: idempotencyKey,
		Phone:          header.PhoneNumber,
		MsgID:          header.MsgID,
		Serial:         serial,
		Payload:        encoded[len(headerBytes):],
		Status:         repository.CommandStatusPending,
		// initialWait 覆盖会话重传窗口，到期前不重投，避免与仍在重传的报文重复。
		NextAttemptAt: now.Add(outbox.InitialWait),
		ExpiresAt:     now.Add(outbox.ExpireAfter),
	})
	if err != nil {
		return nil, false, fmt.Errorf("enqueue downlink command: %w", err)
	}
	if !created {
		return command, false, nil
	}

	if err := serv.Send(session.ID, msg, server.WithSerial(serial)); err != nil {
		// 首次下发失败不直接判死：保持 pending 并安排重投，设备重新上线后仍可补发。
		next := time.Now().Add(outbox.RetryBase)
		_, _ = commands.ScheduleRetry(c.Request.Context(), header.PhoneNumber, header.MsgID, serial, next, err.Error())
		return command, true, fmt.Errorf("send downlink command: %w", err)
	}
	return command, true, nil
}

// writeCommandAccepted 返回下行命令受理结果；幂等命中用 200 与 202 区分。
func writeCommandAccepted(c *gin.Context, command *repository.DownlinkCommand, created bool, fields gin.H) {
	fields["commandId"] = command.CommandID
	fields["duplicate"] = !created
	status := http.StatusAccepted
	if !created {
		status = http.StatusOK
	}
	c.JSON(status, fields)
}

func newCommandID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate command id: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}
