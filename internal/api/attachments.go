package api

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/fakeyanss/jt808-server-go/internal/contracts/attachment"
	"github.com/fakeyanss/jt808-server-go/internal/control"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

func (h *handler) postDeviceAttachmentsUpload(c *gin.Context) {
	var request postDeviceAttachmentsUploadRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
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

	phone := c.Param("phone")
	device, err := h.devices.GetByPhone(c.Request.Context(), phone)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}

	ip := strings.TrimSpace(request.IP)
	tcpPort := request.TCPPort
	udpPort := request.UDPPort
	if h.alarmServ != nil {
		// 同进程附件节点：未显式指定端口时回落到实际监听端口。
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
		if tcpPort == 0 {
			tcpPort = resolvePort(h.alarmServ.Addr())
		}
		if udpPort == 0 {
			udpPort = resolvePort(h.alarmServ.UDPAddr())
		}
	} else {
		if h.attachmentAllocator == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"code": attachment.AttachmentCodeNoNodes,
				"err":  "no attachment node is configured: set cluster.attachment.nodes or run role attachment",
			})
			return
		}
		lease, allocErr := h.attachmentAllocator.Allocate(c.Request.Context(), attachment.AttachmentLeaseRequest{
			Phone: phone, AlarmID: request.AlarmNo,
		})
		if allocErr != nil {
			code := attachment.AttachmentCodeNodesUnavailable
			var unavailable *attachment.AttachmentUnavailableError
			if errors.As(allocErr, &unavailable) {
				code = unavailable.Code
			}
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": code, "err": allocErr.Error()})
			return
		}
		ip = lease.Endpoint.IP
		tcpPort = lease.Endpoint.TCPPort
		udpPort = lease.Endpoint.UDPPort
	}
	if ip == "" || len(ip) > 255 {
		c.JSON(http.StatusBadRequest, gin.H{"err": "ip must contain 1 to 255 bytes"})
		return
	}
	if tcpPort == 0 && udpPort == 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code": attachment.AttachmentCodeNodesUnavailable,
			"err":  "attachment node has no tcp or udp listener",
		})
		return
	}

	session, err := downlinkSession(c.Request.Context(), h.connRegistry, device, h.outboxEnabled)
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
	command, created, err := dispatchDownlink(c, h.downlink, session, msg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{"messageId": "0x9208", "ip": ip, "tcpPort": tcpPort, "udpPort": udpPort})

}

func (h *handler) postDeviceFtp1078Upload(c *gin.Context) {
	if h.ftpTasks == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"err": "ftp1078 is not enabled on this process"})
		return
	}
	var request postDeviceFtp1078UploadRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	if err := request.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	if h.ftpPort == 0 || strings.TrimSpace(h.ftpAdvertiseIP) == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"err": "ftp1078 advertise ip/port is not configured"})
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
	serial := nextDownlinkSerial(session)
	uploadPath := strings.TrimSpace(request.UploadPath)
	if uploadPath == "" {
		uploadPath = fmt.Sprintf("/%s/%d", phone, serial)
	}
	task, password, err := h.ftpTasks.Create(c.Request.Context(), control.CreateFTPTaskInput{
		Phone: phone, Serial: serial, UploadPath: uploadPath, Channel: request.Channel,
		StartTime: *request.StartTime, EndTime: *request.EndTime, AlarmSign: fmt.Sprintf("%016X", request.AlarmSign),
		MediaType: request.MediaType, StreamType: request.StreamType,
		StorageType: request.StorageType, TaskCondition: request.TaskCondition,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	msg := &model.Msg9206{
		Header:         model.GenMsgHeader(device, 0x9206, serial),
		ServerIP:       h.ftpAdvertiseIP,
		Port:           h.ftpPort,
		Username:       task.Username,
		Password:       password,
		UploadPath:     task.UploadPath,
		LogicChannelID: request.Channel,
		StartTime:      request.StartTime,
		EndTime:        request.EndTime,
		AlarmSign:      request.AlarmSign,
		MediaType:      request.MediaType,
		StreamType:     request.StreamType,
		StorageType:    request.StorageType,
		TaskCondition:  request.TaskCondition,
	}
	command, created, err := dispatchDownlink(c, h.downlink, session, msg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{
		"messageId": "0x9206", "serial": serial, "username": task.Username,
		"uploadPath": task.UploadPath, "ip": h.ftpAdvertiseIP, "port": h.ftpPort,
	})

}

func (h *handler) postDeviceFtp1078Control(c *gin.Context) {
	if h.ftpTasks == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"err": "ftp1078 is not enabled on this process"})
		return
	}
	var request postDeviceFtp1078ControlRequest
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
	if _, err := h.ftpTasks.Task(c.Request.Context(), phone, request.Serial); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"err": "ftp1078 task not found"})
		return
	}
	session, err := downlinkSession(c.Request.Context(), h.connRegistry, device, h.outboxEnabled)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	msg := &model.Msg9207{
		Header:             model.GenMsgHeader(device, 0x9207, nextDownlinkSerial(session)),
		AnswerSerialNumber: request.Serial,
		Control:            request.Action,
	}
	command, created, err := dispatchDownlink(c, h.downlink, session, msg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	task, err := h.ftpTasks.ApplyControl(c.Request.Context(), phone, request.Serial, request.Action)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
		return
	}
	writeCommandAccepted(c, command, created, gin.H{
		"messageId": "0x9207", "serial": request.Serial, "status": task.Status,
	})

}

func (h *handler) getDeviceFtp1078Task(c *gin.Context) {
	if h.ftpTasks == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"err": "ftp1078 is not enabled on this process"})
		return
	}
	serial, err := strconv.ParseUint(c.Param("serial"), 10, 16)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": "serial must be a 16-bit unsigned integer"})
		return
	}
	task, err := h.ftpTasks.Task(c.Request.Context(), c.Param("phone"), uint16(serial))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"err": "ftp1078 task not found"})
		return
	}
	c.JSON(http.StatusOK, task)

}

func (h *handler) getDeviceAttachments(c *gin.Context) {
	phone := c.Param("phone")
	if _, err := h.devices.GetByPhone(c.Request.Context(), phone); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	if h.attachments != nil {
		items, err := h.attachments.ListByPhone(c.Request.Context(), repository.AttachmentSourceSubia, phone, 200)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"phone": phone, "count": len(items), "items": items})
		return
	}
	if h.alarmServ == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"err": "attachment service is not enabled"})
		return
	}
	c.JSON(http.StatusOK, h.alarmServ.Assembler().Select(phone))

}

func (h *handler) getDeviceAttachment(c *gin.Context) {
	phone := c.Param("phone")
	if _, err := h.devices.GetByPhone(c.Request.Context(), phone); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	if h.attachments != nil {
		record, err := h.attachments.FindByFileName(c.Request.Context(), repository.AttachmentSourceSubia, phone, c.Param("fileName"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"err": "attachment file not found"})
			return
		}
		c.JSON(http.StatusOK, record)
		return
	}
	if h.alarmServ == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"err": "attachment service is not enabled"})
		return
	}
	meta, err := h.alarmServ.Assembler().FindMetadata(phone, c.Param("fileName"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"err": err.Error()})
		return
	}
	c.JSON(http.StatusOK, meta)

}

func (h *handler) getDeviceAttachmentsContent(c *gin.Context) {
	phone := c.Param("phone")
	if _, err := h.devices.GetByPhone(c.Request.Context(), phone); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		return
	}
	fileName := c.Param("fileName")
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
	if h.attachments != nil {
		record, err := h.attachments.FindByFileName(c.Request.Context(), repository.AttachmentSourceSubia, phone, fileName)
		if err != nil || record.Status != repository.AttachmentStatusCompleted {
			c.JSON(http.StatusNotFound, gin.H{"err": "attachment file is not completed"})
			return
		}
		if h.blob == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"err": "object store is not configured"})
			return
		}
		c.Status(http.StatusOK)
		for _, segment := range record.Segments {
			reader, openErr := h.blob.OpenRange(c.Request.Context(), segment.Key, 0, segment.Size)
			if openErr != nil {
				log.Warn().Err(openErr).Str("key", segment.Key).Msg("Fail to read attachment segment")
				return
			}
			_, copyErr := io.Copy(c.Writer, reader)
			_ = reader.Close()
			if copyErr != nil {
				return
			}
		}
		return
	}
	if h.alarmServ == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"err": "attachment service is not enabled"})
		return
	}
	meta, err := h.alarmServ.Assembler().FindMetadata(phone, fileName)
	if err != nil || meta.Path == "" {
		c.JSON(http.StatusNotFound, gin.H{"err": "attachment file is not found"})
		return
	}
	http.ServeFile(c.Writer, c.Request, meta.Path)

}
