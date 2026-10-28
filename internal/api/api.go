package api

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mitchellh/mapstructure"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/server"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

func Run(serv *server.TCPServer, cfg *config.Config) {
	// web server structure
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	cache := storage.GetDeviceCache()
	geoCache := storage.GetGeoCache()

	router.GET("/device", func(c *gin.Context) {
		c.JSON(http.StatusOK, cache.ListDevice())
	})

	router.GET("/device/:phone/geo", func(c *gin.Context) {
		phone := c.Param("phone")

		device, err := cache.GetDeviceByPhone(phone)
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
		gis, err := geoCache.GetGeoLatestByPhone(phone)
		if err != nil {
			return
		}
		res["gis"] = gis

		c.JSON(http.StatusOK, res)
	})

	router.GET("/device/:phone/params", func(c *gin.Context) {
		phone := c.Param("phone")
		device, err := cache.GetDeviceByPhone(phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := storage.GetSession(device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		serial := session.GetNextSerialNum()
		responses := storage.GetParamResponseBroker()
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

	router.GET("/device/:phone/properties", func(c *gin.Context) {
		phone := c.Param("phone")
		device, err := cache.GetDeviceByPhone(phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := storage.GetSession(device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		responses := storage.GetPropertyResponseBroker()
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

	router.GET("/device/:phone/location", func(c *gin.Context) {
		phone := c.Param("phone")
		device, err := cache.GetDeviceByPhone(phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := storage.GetSession(device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		serial := session.GetNextSerialNum()
		responses := storage.GetLocationResponseBroker()
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

	router.POST("/device/:phone/control", func(c *gin.Context) {
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
		device, err := cache.GetDeviceByPhone(phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := storage.GetSession(device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		if len(request.CommandParam) > 256 {
			c.JSON(http.StatusBadRequest, gin.H{"err": "commandParam exceeds 256 UTF-8 bytes"})
			return
		}
		msg := &model.Msg8105{Header: model.GenMsgHeader(device, 0x8105, session.GetNextSerialNum()), CommandWord: request.CommandWord, CommandParam: request.CommandParam}
		serv.Send(session.ID, msg)
		c.JSON(http.StatusAccepted, gin.H{"messageId": "0x8105", "commandWord": request.CommandWord})
	})

	router.POST("/device/:phone/alarms/confirm", func(c *gin.Context) {
		var request struct {
			AlarmSerial uint16 `json:"alarmSerial"`
			AlarmType   uint32 `json:"alarmType"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		phone := c.Param("phone")
		device, err := cache.GetDeviceByPhone(phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := storage.GetSession(device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		msg := &model.Msg8203{Header: model.GenMsgHeader(device, 0x8203, session.GetNextSerialNum()), AlarmSerial: request.AlarmSerial, AlarmType: request.AlarmType}
		storage.GetAlarmCache().RegisterConfirmation(phone, msg.Header.SerialNumber, request.AlarmSerial, request.AlarmType)
		serv.Send(session.ID, msg)
		c.JSON(http.StatusAccepted, gin.H{"messageId": "0x8203", "alarmSerial": request.AlarmSerial, "alarmType": request.AlarmType})
	})

	router.GET("/device/:phone/alarms", func(c *gin.Context) {
		phone := c.Param("phone")
		if _, err := cache.GetDeviceByPhone(phone); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, storage.GetAlarmCache().List(phone))
	})

	router.PUT("/device/:phone/params", func(c *gin.Context) {
		phone := c.Param("phone")
		params := model.DeviceParams{}
		if err := c.ShouldBind(&params); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		device, err := cache.GetDeviceByPhone(phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := storage.GetSession(device.SessionID)
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

	httpAddr := ":" + cfg.Server.Port.HTTPPort

	log.Debug().Msgf("Listening and serving HTTP on :%s", cfg.Server.Port.HTTPPort)
	err := router.Run(httpAddr)
	if err != nil {
		log.Error().Err(err).Str("addr", httpAddr).Msg("Fail to run gin router")
		os.Exit(1)
	}
}
