package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/domain"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/mitchellh/mapstructure"
	"github.com/rs/zerolog/log"
)

func (h *handler) getDevice(c *gin.Context) {
	list, err := h.devices.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)

}

func (h *handler) getDeviceReports(c *gin.Context) {
	phone := c.Param("phone")
	if _, err := h.devices.GetByPhone(c.Request.Context(), phone); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	industry := h.repos.Industry
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
		upgrade, err := h.repos.UpgradeResults.Latest(c.Request.Context(), phone)
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

}

func (h *handler) getDeviceGeo(c *gin.Context) {
	phone := c.Param("phone")

	device, err := h.devices.GetByPhone(c.Request.Context(), phone)
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
	gis, err := h.geo.Latest(c.Request.Context(), phone)
	if err != nil {
		return
	}
	res["gis"] = gis

	c.JSON(http.StatusOK, res)

}

func (h *handler) getDeviceParams(c *gin.Context) {
	phone := c.Param("phone")
	device, err := h.devices.GetByPhone(c.Request.Context(), phone)
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
	if h.queryer != nil {
		body, queryErr := h.queryer.Query(c.Request.Context(), phone, msg, 0x0104, 10*time.Second)
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

	session, err := localQuerySession(c.Request.Context(), h.connRegistry, device)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	serial := nextDownlinkSerial(session)
	msg.GetHeader().SerialNumber = serial
	responses := h.runtimeState.ParamResponses
	responseCh, cancel, err := responses.Register(phone, serial)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"err": err.Error()})
		return
	}
	defer cancel()

	directSend(c, h.sender, msg)
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

}

func (h *handler) getDeviceProperties(c *gin.Context) {
	phone := c.Param("phone")
	device, err := h.devices.GetByPhone(c.Request.Context(), phone)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	msg := &model.Msg8107{Header: model.GenMsgHeader(device, 0x8107, 0)}
	if h.queryer != nil {
		body, queryErr := h.queryer.Query(c.Request.Context(), phone, msg, 0x0107, 10*time.Second)
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
	session, err := localQuerySession(c.Request.Context(), h.connRegistry, device)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	msg.Header.SerialNumber = nextDownlinkSerial(session)
	responses := h.runtimeState.PropertyResponses
	responseCh, cancel, err := responses.Register(phone)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"err": err.Error()})
		return
	}
	defer cancel()
	directSend(c, h.sender, msg)
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

}

func (h *handler) getDeviceLocation(c *gin.Context) {
	phone := c.Param("phone")
	device, err := h.devices.GetByPhone(c.Request.Context(), phone)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	msg := &model.Msg8201{Header: model.GenMsgHeader(device, 0x8201, 0)}
	if h.queryer != nil {
		body, queryErr := h.queryer.Query(c.Request.Context(), phone, msg, 0x0201, 10*time.Second)
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
	session, err := localQuerySession(c.Request.Context(), h.connRegistry, device)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	serial := nextDownlinkSerial(session)
	msg.Header.SerialNumber = serial
	responses := h.runtimeState.LocationResponses
	responseCh, cancel, err := responses.Register(phone, serial)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"err": err.Error()})
		return
	}
	defer cancel()
	directSend(c, h.sender, msg)
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

}

func (h *handler) getDeviceAlarms(c *gin.Context) {
	phone := c.Param("phone")
	if _, err := h.devices.GetByPhone(c.Request.Context(), phone); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	alarms, err := h.repos.Alarms.List(c.Request.Context(), phone)
	if err != nil {
		log.Error().Err(err).Str("phone", phone).Msg("list alarms")
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	c.JSON(http.StatusOK, alarms)

}

func (h *handler) putDeviceParams(c *gin.Context) {
	phone := c.Param("phone")
	params := domain.DeviceParams{}
	if err := c.ShouldBind(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	device, err := h.devices.GetByPhone(c.Request.Context(), phone)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	session, err := downlinkSession(c.Request.Context(), h.connRegistry, device, h.outboxEnabled)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	header := model.GenMsgHeader(device, 0x8103, nextDownlinkSerial(session))
	msg := model.Msg8103{
		Header:     header,
		Parameters: &params,
	}
	command, created, err := dispatchDownlink(c, h.downlink, session, &msg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8103"})

}
