package api

import (
	"context"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/gin-gonic/gin"
)

func (h *handler) postDeviceRegionsDelete(c *gin.Context) {
	var request postDeviceRegionsDeleteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	if err := request.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	phone := c.Param("phone")
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
	msg := &model.Msg860Delete{Header: model.GenMsgHeader(device, request.MessageID, nextDownlinkSerial(session)), IDs: request.IDs}
	command, created, err := dispatchDownlink(c, h.downlink, session, msg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": request.MessageID, "count": len(request.IDs)})

}

func (h *handler) postDeviceRegionsSet(c *gin.Context) {
	var request postDeviceRegionsSetRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	if err := request.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	phone := c.Param("phone")
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
	command, created, err := dispatchDownlink(c, h.downlink, session, msg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": request.MessageID, "count": count})

}

func (h *handler) postDeviceRegionsQuery(c *gin.Context) {
	var request postDeviceRegionsQueryRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	if err := request.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	phone := c.Param("phone")
	device, err := h.devices.GetByPhone(c.Request.Context(), phone)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	if h.queryer != nil {
		msg := &model.Msg8608{Header: model.GenMsgHeader(device, 0x8608, 0), QueryType: request.QueryType, IDs: request.IDs}
		body, queryErr := h.queryer.Query(c.Request.Context(), phone, msg, 0x0608, 10*time.Second)
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
	session, err := localQuerySession(c.Request.Context(), h.connRegistry, device)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	broker := h.runtimeStores.RegionResponses
	response, cancel, err := broker.Register(phone, request.QueryType)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"err": err.Error()})
		return
	}
	defer cancel()
	directSend(c, h.sender, &model.Msg8608{Header: model.GenMsgHeader(device, 0x8608, nextDownlinkSerial(session)), QueryType: request.QueryType, IDs: request.IDs})
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

}
