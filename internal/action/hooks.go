// Package action 定义协议生命周期钩子，用于把业务数据从协议内核中解耦。
//
// 钩子方法均不返回错误：实现方只做旁路通知，任何失败都不得影响协议主流程。
package action

import "context"

// Hooks 是终端接入过程中的最小生命周期钩子集合。
// 后续 M3/M4 可按需扩展（例如参数应答、媒体、鉴权失败原因等），当前只保留最小点位。
type Hooks interface {
	OnRegister(ctx context.Context, event RegisterEvent)
	OnAuth(ctx context.Context, event AuthEvent)
	OnLocation(ctx context.Context, event LocationEvent)
	OnAlarm(ctx context.Context, event AlarmEvent)
	OnDisconnect(ctx context.Context, event DisconnectEvent)
	OnWaybill(ctx context.Context, event WaybillEvent)
	OnDriverIdentity(ctx context.Context, event DriverIdentityEvent)
	OnCANBus(ctx context.Context, event CANBusEvent)
	OnTachograph(ctx context.Context, event TachographEvent)
	OnEventReport(ctx context.Context, event EventReportEvent)
	OnVehicleControlAck(ctx context.Context, event VehicleControlAckEvent)
	OnTerminalUpgradeResult(ctx context.Context, event TerminalUpgradeResultEvent)
	OnMediaEvent(ctx context.Context, event MediaEvent)
	OnMediaUpload(ctx context.Context, event MediaUploadEvent)
	OnFileUploadComplete(ctx context.Context, event FileUploadCompleteEvent)
	OnAttachmentInfo(ctx context.Context, event AttachmentInfoEvent)
}

// RegisterEvent 终端注册（0x0100）事件。
type RegisterEvent struct {
	EventID         string `json:"eventId"`
	Phone           string `json:"phone"`
	DeviceID        string `json:"deviceId"`
	Plate           string `json:"plate"`
	ProtocolVersion uint8  `json:"protocolVersion"`
	VersionDesc     string `json:"versionDesc"`
	Result          uint8  `json:"result"` // 0x8100 应答结果，0 表示成功
}

// AuthEvent 终端鉴权（0x0102）事件。
type AuthEvent struct {
	EventID         string `json:"eventId"`
	Phone           string `json:"phone"`
	IMEI            string `json:"imei"`
	SoftwareVersion string `json:"softwareVersion"`
	Success         bool   `json:"success"`
}

// LocationEvent 位置上报（0x0200 / 0x0704）事件。
type LocationEvent struct {
	EventID    string `json:"eventId"`
	Phone      string `json:"phone"`
	Serial     uint16 `json:"serial"`
	AlarmSign  uint32 `json:"alarmSign"`
	StatusSign uint32 `json:"statusSign"`
	Latitude   uint32 `json:"latitude"`
	Longitude  uint32 `json:"longitude"`
	Altitude   uint16 `json:"altitude"`
	Speed      uint16 `json:"speed"`
	Direction  uint16 `json:"direction"`
	Time       string `json:"time"`
}

// AlarmEvent 报警事件，来源是位置上报中非零的报警标志位。
type AlarmEvent struct {
	EventID   string `json:"eventId"`
	Phone     string `json:"phone"`
	Serial    uint16 `json:"serial"`
	AlarmSign uint32 `json:"alarmSign"`
	Latitude  uint32 `json:"latitude"`
	Longitude uint32 `json:"longitude"`
	Time      string `json:"time"`
	// ExtItems 是 0x0200 附加信息的结构化解析结果，元素类型由协议层定义，
	// 每个元素通过 extType 字段区分 adas/dsm/tpms/bsd/raw。
	ExtItems []any `json:"extItems,omitempty"`
	// ExtRaw 汇总无法结构化解析的附加信息项原始字节（仅 raw 兜底项）。
	ExtRaw []byte `json:"extRaw,omitempty"`
	// AlarmID 是 0x0200 扩展项（0x64~0x67）中提取的苏标 16 字节报警标识号。
	// 取不到时为 nil；调用方不得自行拼装该标识号。
	AlarmID []byte `json:"alarmId,omitempty"`
}

// DisconnectEvent 连接断开事件。
type DisconnectEvent struct {
	EventID   string `json:"eventId"`
	Phone     string `json:"phone,omitempty"`
	SessionID string `json:"sessionId"`
	Transport string `json:"transport"`
	// Reason 区分会话结束原因：logout（0x0003）或 transport_closed（连接关闭/超时）。
	Reason string `json:"reason,omitempty"`
}

// 以下事件是一期扩展业务事件，字段名与单位按 JT/T 808 与苏标原文。

// WaybillEvent 电子运单（0x0701）。
type WaybillEvent struct {
	EventID string `json:"eventId"`
	Phone   string `json:"phone"`
	Serial  uint16 `json:"serial"`
	Time    string `json:"time"`
	Content string `json:"content"` // 电子运单内容，GBK 解码后的文本
}

// DriverIdentityEvent 驾驶员身份信息（0x0702）。
type DriverIdentityEvent struct {
	EventID           string `json:"eventId"`
	Phone             string `json:"phone"`
	Serial            uint16 `json:"serial"`
	Time              string `json:"time"`
	State             uint8  `json:"state"` // 1:上岗 2:下班
	ReadResult        uint8  `json:"readResult,omitempty"`
	Name              string `json:"name,omitempty"`
	QualificationCode string `json:"qualificationCode,omitempty"`
	IssuingAuthority  string `json:"issuingAuthority,omitempty"`
	ValidUntil        string `json:"validUntil,omitempty"`
	IdentityNumber    string `json:"identityNumber,omitempty"`
}

// CANDataItem 是 0x0705 中的一条 CAN 数据。
type CANDataItem struct {
	CANID uint32 `json:"canId"`
	Data  []byte `json:"data"` // 8 字节
}

// CANBusEvent CAN 总线数据（0x0705）。
type CANBusEvent struct {
	EventID string        `json:"eventId"`
	Phone   string        `json:"phone"`
	Serial  uint16        `json:"serial"`
	Time    string        `json:"time"` // 首个 CAN 数据的时间
	Count   uint16        `json:"count"`
	Items   []CANDataItem `json:"items"`
}

// TachographEvent 行驶记录仪数据（0x0700）。
type TachographEvent struct {
	EventID      string `json:"eventId"`
	Phone        string `json:"phone"`
	Serial       uint16 `json:"serial"`
	Time         string `json:"time"`
	AnswerSerial uint16 `json:"answerSerial"` // 对应平台下发命令的流水号
	Command      uint8  `json:"command"`      // 命令字
	Data         []byte `json:"data,omitempty"`
}

// EventReportEvent 事件报告（0x0301）。
type EventReportEvent struct {
	EventID  string `json:"eventId"`
	Phone    string `json:"phone"`
	Serial   uint16 `json:"serial"`
	Time     string `json:"time"`
	ReportID uint8  `json:"reportId"` // 协议中的"事件ID"
}

// VehicleControlAckEvent 车辆控制应答（0x0500）。
type VehicleControlAckEvent struct {
	EventID      string `json:"eventId"`
	Phone        string `json:"phone"`
	Serial       uint16 `json:"serial"`
	Time         string `json:"time"`
	ControlFlags uint16 `json:"controlFlags"` // 回显 0x8500 的控制标志
}

// TerminalUpgradeResultEvent 终端升级结果（0x0108）。
type TerminalUpgradeResultEvent struct {
	EventID     string `json:"eventId"`
	Phone       string `json:"phone"`
	Serial      uint16 `json:"serial"`
	Time        string `json:"time"`
	UpgradeType uint8  `json:"upgradeType"`
	Result      uint8  `json:"result"` // 0:成功 1:失败 2:取消
}

// MediaEvent 多媒体事件（0x0800）。
type MediaEvent struct {
	EventID             string `json:"eventId"`
	Phone               string `json:"phone"`
	Serial              uint16 `json:"serial"`
	Time                string `json:"time"`
	MultiMediaID        uint32 `json:"multiMediaId"`
	MultiMediaType      uint8  `json:"multiMediaType"`      // 0:图像 1:音频 2:视频
	MultiMediaContainer uint8  `json:"multiMediaContainer"` // 0:JPEG 1:TIF 2:MP3 3:WAV 4:WMV
	MediaEventID        uint8  `json:"mediaEventId"`        // 协议中的"事件项编码"
	LogicChannelID      uint8  `json:"logicChannelId"`
}

// MediaUploadEvent 多媒体数据上传（0x0801）。只上报元数据，不含媒体字节。
type MediaUploadEvent struct {
	EventID             string `json:"eventId"`
	Phone               string `json:"phone"`
	Serial              uint16 `json:"serial"`
	Time                string `json:"time"`
	MultiMediaID        uint32 `json:"multiMediaId"`
	MultiMediaType      uint8  `json:"multiMediaType"`
	MultiMediaContainer uint8  `json:"multiMediaContainer"`
	MediaEventID        uint8  `json:"mediaEventId"`
	LogicChannelID      uint8  `json:"logicChannelId"`
	FragmentBytes       int    `json:"fragmentBytes"` // 本包媒体数据字节数
}

// FileUploadCompleteEvent 文件上传完成通知（0x1206）。
type FileUploadCompleteEvent struct {
	EventID            string `json:"eventId"`
	Phone              string `json:"phone"`
	Serial             uint16 `json:"serial"`
	Time               string `json:"time"`
	AnswerSerialNumber uint16 `json:"answerSerialNumber"`
	Result             uint8  `json:"result"` // 0:成功 1:失败
}

// AttachmentEntry 是 0x1210 附件清单中的一项。
type AttachmentEntry struct {
	FileName string `json:"fileName"`
	FileSize uint32 `json:"fileSize"`
}

// AttachmentInfoEvent 报警附件信息（0x1210）。
type AttachmentInfoEvent struct {
	EventID         string            `json:"eventId"`
	Phone           string            `json:"phone"`
	Serial          uint16            `json:"serial"`
	Time            string            `json:"time"`
	TerminalID      string            `json:"terminalId"`
	AlarmSign       []byte            `json:"alarmSign"`
	AlarmNo         string            `json:"alarmNo"`
	InfoType        uint8             `json:"infoType"`
	AttachmentCount uint8             `json:"attachmentCount"`
	Attachments     []AttachmentEntry `json:"attachments"`
}

// Noop 是默认钩子实现，所有回调均为空操作。
type Noop struct{}

func (Noop) OnRegister(context.Context, RegisterEvent)     {}
func (Noop) OnAuth(context.Context, AuthEvent)             {}
func (Noop) OnLocation(context.Context, LocationEvent)     {}
func (Noop) OnAlarm(context.Context, AlarmEvent)           {}
func (Noop) OnDisconnect(context.Context, DisconnectEvent) {}

func (Noop) OnWaybill(context.Context, WaybillEvent) {}

func (Noop) OnDriverIdentity(context.Context, DriverIdentityEvent) {}

func (Noop) OnCANBus(context.Context, CANBusEvent) {}

func (Noop) OnTachograph(context.Context, TachographEvent) {}

func (Noop) OnEventReport(context.Context, EventReportEvent) {}

func (Noop) OnVehicleControlAck(context.Context, VehicleControlAckEvent) {}

func (Noop) OnTerminalUpgradeResult(context.Context, TerminalUpgradeResultEvent) {}

func (Noop) OnMediaEvent(context.Context, MediaEvent) {}

func (Noop) OnMediaUpload(context.Context, MediaUploadEvent) {}

func (Noop) OnFileUploadComplete(context.Context, FileUploadCompleteEvent) {}

func (Noop) OnAttachmentInfo(context.Context, AttachmentInfoEvent) {}
