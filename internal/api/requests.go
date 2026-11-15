package api

import (
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// postDeviceControlRequest 是 postDeviceControl 的请求体。
type postDeviceControlRequest struct {
	CommandWord  uint8                    `json:"commandWord" binding:"required"`
	CommandParam string                   `json:"commandParam"`
	Parameters   []model.CommandParameter `json:"parameters"`
}

// postDeviceRecorderCommandRequest 是 postDeviceRecorderCommand 的请求体。
type postDeviceRecorderCommandRequest struct {
	MessageID uint16 `json:"messageId" binding:"required"`
	Command   uint8  `json:"command"`
	DataHex   string `json:"dataHex"`
}

// postDeviceMediaResourcesRequest 是 postDeviceMediaResources 的请求体。
type postDeviceMediaResourcesRequest struct {
	LogicChannelID uint8  `json:"logicChannelId"`
	StartTime      string `json:"startTime"`
	EndTime        string `json:"endTime"`
	AlarmSign      uint32 `json:"alarmSign"`
	AlarmSignExt   uint32 `json:"alarmSignExt"`
	MediaType      uint8  `json:"mediaType"`
	StreamType     uint8  `json:"streamType"`
	StorageType    uint8  `json:"storageType"`
}

// postDeviceMediaCommandRequest 是 postDeviceMediaCommand 的请求体。
type postDeviceMediaCommandRequest struct {
	MessageID uint16 `json:"messageId" binding:"required"`
	BodyHex   string `json:"bodyHex"`
}

// postDeviceAlarmsConfirmRequest 是 postDeviceAlarmsConfirm 的请求体。
type postDeviceAlarmsConfirmRequest struct {
	AlarmSerial uint16 `json:"alarmSerial"`
	AlarmType   uint32 `json:"alarmType"`
}

// postDeviceRegionsDeleteRequest 是 postDeviceRegionsDelete 的请求体。
type postDeviceRegionsDeleteRequest struct {
	MessageID uint16   `json:"messageId" binding:"required"`
	IDs       []uint32 `json:"ids"`
}

// postDeviceRegionsSetRequest 是 postDeviceRegionsSet 的请求体。
type postDeviceRegionsSetRequest struct {
	MessageID   uint16                `json:"messageId" binding:"required"`
	SettingAttr uint8                 `json:"settingAttr"`
	Circles     []model.CircleArea    `json:"circles"`
	Rectangles  []model.RectangleArea `json:"rectangles"`
	Polygons    []model.PolygonArea   `json:"polygons"`
	Routes      []model.Route         `json:"routes"`
	BodyHex     string                `json:"bodyHex"`
}

// postDeviceRegionsQueryRequest 是 postDeviceRegionsQuery 的请求体。
type postDeviceRegionsQueryRequest struct {
	QueryType uint8    `json:"queryType" binding:"required,min=1,max=4"`
	IDs       []uint32 `json:"ids"`
}

// postDeviceAttachmentsUploadRequest 是 postDeviceAttachmentsUpload 的请求体。
type postDeviceAttachmentsUploadRequest struct {
	IP           string `json:"ip"`
	TCPPort      uint16 `json:"tcpPort"`
	UDPPort      uint16 `json:"udpPort"`
	AlarmSignHex string `json:"alarmSignHex" binding:"required"`
	AlarmNo      string `json:"alarmNo" binding:"required"`
	ReservedHex  string `json:"reservedHex"`
}

// postDeviceFtp1078UploadRequest 是 postDeviceFtp1078Upload 的请求体。
type postDeviceFtp1078UploadRequest struct {
	UploadPath    string     `json:"uploadPath"`
	Channel       uint8      `json:"channel"`
	StartTime     *time.Time `json:"startTime"`
	EndTime       *time.Time `json:"endTime"`
	AlarmSign     uint64     `json:"alarmSign"`
	MediaType     uint8      `json:"mediaType"`
	StreamType    uint8      `json:"streamType"`
	StorageType   uint8      `json:"storageType"`
	TaskCondition uint8      `json:"taskCondition"`
}

// postDeviceFtp1078ControlRequest 是 postDeviceFtp1078Control 的请求体。
type postDeviceFtp1078ControlRequest struct {
	Serial uint16 `json:"serial" binding:"required"`
	Action uint8  `json:"action"`
}
