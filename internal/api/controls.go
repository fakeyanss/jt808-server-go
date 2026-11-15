package api

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	protocolhex "github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

func (h *handler) postDeviceControl(c *gin.Context) {
	var request postDeviceControlRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	if err := request.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
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
	command, created, err := dispatchDownlink(c, h.downlink, session, msg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8105", "commandWord": request.CommandWord})

}

func (h *handler) postDeviceDriverQuery(c *gin.Context) {
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
	directSend(c, h.sender, &model.Msg8702{Header: model.GenMsgHeader(device, 0x8702, nextDownlinkSerial(session))})
	c.JSON(http.StatusAccepted, gin.H{"messageId": "0x8702"})

}

func (h *handler) postDeviceRecorderCommand(c *gin.Context) {
	var request postDeviceRecorderCommandRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	if err := request.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	data, err := hex.DecodeString(request.DataHex)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": "dataHex must be an even-length hexadecimal string"})
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
	msg := &model.MsgRecordCommand{Header: model.GenMsgHeader(device, request.MessageID, nextDownlinkSerial(session)), Command: request.Command, Data: data}
	command, created, err := dispatchDownlink(c, h.downlink, session, msg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": request.MessageID, "command": request.Command})

}

func (h *handler) postDeviceMediaCapture(c *gin.Context) {
	var request model.Msg8801
	if err := c.ShouldBindJSON(&request); err != nil {
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
	request.Header = model.GenMsgHeader(device, 0x8801, nextDownlinkSerial(session))
	if _, err := request.Encode(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": "invalid camera command fields"})
		return
	}
	command, created, err := dispatchDownlink(c, h.downlink, session, &request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8801"})

}

func (h *handler) postDeviceMediaSearch(c *gin.Context) {
	var request model.Msg8802
	if err := c.ShouldBindJSON(&request); err != nil {
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
	request.Header = model.GenMsgHeader(device, 0x8802, nextDownlinkSerial(session))
	if _, err := request.Encode(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": "invalid media search fields"})
		return
	}
	directSend(c, h.sender, &request)
	c.JSON(http.StatusAccepted, gin.H{"messageId": "0x8802"})

}

func (h *handler) postDeviceMediaResources(c *gin.Context) {
	var request postDeviceMediaResourcesRequest
	if err := c.ShouldBindJSON(&request); err != nil {
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
	directSend(c, h.sender, &msg)
	c.JSON(http.StatusAccepted, gin.H{"messageId": "0x9205"})

}

func (h *handler) postDeviceMediaCommand(c *gin.Context) {
	var request postDeviceMediaCommandRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	if err := request.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	body, err := hex.DecodeString(request.BodyHex)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": "bodyHex must be an even-length hexadecimal string"})
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
	command, created, err := dispatchDownlink(c, h.downlink, session, msg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": fmt.Sprintf("0x%04X", request.MessageID)})

}

func (h *handler) postDeviceTextIssue(c *gin.Context) {
	var request model.Msg8300
	if err := c.ShouldBindJSON(&request); err != nil {
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
	request.Header = model.GenMsgHeader(device, 0x8300, nextDownlinkSerial(session))
	if _, err := request.Encode(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": "invalid text issue fields"})
		return
	}
	command, created, err := dispatchDownlink(c, h.downlink, session, &request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8300"})

}

func (h *handler) postDeviceEventsSet(c *gin.Context) {
	var request model.Msg8301
	if err := c.ShouldBindJSON(&request); err != nil {
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
	request.ItemCount = uint8(len(request.Items))
	request.Header = model.GenMsgHeader(device, 0x8301, nextDownlinkSerial(session))
	if _, err := request.Encode(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": "invalid event set fields"})
		return
	}
	command, created, err := dispatchDownlink(c, h.downlink, session, &request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8301"})

}

func (h *handler) postDeviceVehicleControl(c *gin.Context) {
	var request model.Msg8500
	if err := c.ShouldBindJSON(&request); err != nil {
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
	request.Header = model.GenMsgHeader(device, 0x8500, nextDownlinkSerial(session))
	if _, err := request.Encode(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": "invalid vehicle control fields"})
		return
	}
	command, created, err := dispatchDownlink(c, h.downlink, session, &request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8500"})

}

func (h *handler) postDeviceQuestionIssue(c *gin.Context) {
	var request model.Msg8302
	if err := c.ShouldBindJSON(&request); err != nil {
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
	request.Header = model.GenMsgHeader(device, 0x8302, nextDownlinkSerial(session))
	if _, err := request.Encode(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": "invalid question issue fields"})
		return
	}
	command, created, err := dispatchDownlink(c, h.downlink, session, &request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8302"})

}

func (h *handler) postDeviceInfoMenuSet(c *gin.Context) {
	var request model.Msg8303
	if err := c.ShouldBindJSON(&request); err != nil {
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
	request.Header = model.GenMsgHeader(device, 0x8303, nextDownlinkSerial(session))
	if _, err := request.Encode(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": "invalid info menu set fields"})
		return
	}
	command, created, err := dispatchDownlink(c, h.downlink, session, &request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8303"})

}

func (h *handler) postDeviceInfoService(c *gin.Context) {
	var request model.Msg8304
	if err := c.ShouldBindJSON(&request); err != nil {
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
	request.Header = model.GenMsgHeader(device, 0x8304, nextDownlinkSerial(session))
	if _, err := request.Encode(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": "invalid info service fields"})
		return
	}
	command, created, err := dispatchDownlink(c, h.downlink, session, &request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8304"})

}

func (h *handler) postDeviceCallCallback(c *gin.Context) {
	var request model.Msg8400
	if err := c.ShouldBindJSON(&request); err != nil {
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
	request.Header = model.GenMsgHeader(device, 0x8400, nextDownlinkSerial(session))
	if _, err := request.Encode(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": "invalid call callback fields"})
		return
	}
	command, created, err := dispatchDownlink(c, h.downlink, session, &request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8400"})

}

func (h *handler) postDevicePhonebookSet(c *gin.Context) {
	var request model.Msg8401
	if err := c.ShouldBindJSON(&request); err != nil {
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
	request.Header = model.GenMsgHeader(device, 0x8401, nextDownlinkSerial(session))
	if _, err := request.Encode(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": "invalid phonebook set fields"})
		return
	}
	command, created, err := dispatchDownlink(c, h.downlink, session, &request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8401"})

}

func (h *handler) postDeviceTerminalUpgrade(c *gin.Context) {
	var request model.Msg8108
	if err := c.ShouldBindJSON(&request); err != nil {
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
	request.Header = model.GenMsgHeader(device, 0x8108, nextDownlinkSerial(session))
	if _, err := request.Encode(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": "invalid terminal upgrade fields"})
		return
	}
	command, created, err := dispatchDownlink(c, h.downlink, session, &request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8108"})

}

func (h *handler) postDeviceTransparentDown(c *gin.Context) {
	var request model.Msg8900
	if err := c.ShouldBindJSON(&request); err != nil {
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
	request.Header = model.GenMsgHeader(device, 0x8900, nextDownlinkSerial(session))
	if _, err := request.Encode(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": "invalid transparent downlink fields"})
		return
	}
	command, created, err := dispatchDownlink(c, h.downlink, session, &request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": "0x8900"})

}

func (h *handler) postDeviceAlarmsConfirm(c *gin.Context) {
	var request postDeviceAlarmsConfirmRequest
	if err := c.ShouldBindJSON(&request); err != nil {
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
	msg := &model.Msg8203{Header: model.GenMsgHeader(device, 0x8203, nextDownlinkSerial(session)), AlarmSerial: request.AlarmSerial, AlarmType: request.AlarmType}
	if err := h.repos.Alarms.RegisterConfirmation(c.Request.Context(), phone, msg.Header.SerialNumber, request.AlarmSerial, request.AlarmType); err != nil {
		log.Error().Err(err).Str("phone", phone).Msg("register alarm confirmation")
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	directSend(c, h.sender, msg)
	c.JSON(http.StatusAccepted, gin.H{"messageId": "0x8203", "alarmSerial": request.AlarmSerial, "alarmType": request.AlarmType})

}
