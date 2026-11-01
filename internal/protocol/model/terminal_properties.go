package model

import "time"

// TerminalProperties 是 0x0107 终端属性应答的持久化档案（B 类）。
//
// 与 Device 的边界：Device 的静态字段来自 0x0100/0x0102 主动上报，本档案只来自
// 0x8107 查询应答；两者独立存储，避免每张属性应答都触发 devices 全量重写。
type TerminalProperties struct {
	Phone                   string `json:"phone"`
	TerminalType            uint16 `json:"terminalType"`
	ManufacturerID          string `json:"manufacturerId"`
	TerminalModel           string `json:"terminalModel"`
	TerminalID              string `json:"terminalId"`
	ICCID                   string `json:"iccid"`
	HardwareVersion         string `json:"hardwareVersion"`
	FirmwareVersion         string `json:"firmwareVersion"`
	GNSSProperties          uint8  `json:"gnssProperties"`
	CommunicationProperties uint8  `json:"communicationProperties"`
	// Extra 承载协议扩展的长尾属性，落库时序列化为 JSON 兜底列。
	Extra     map[string]string `json:"extra,omitempty"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

// NewTerminalProperties 由 0x0107 应答构造属性档案。
func NewTerminalProperties(in *Msg0107) *TerminalProperties {
	return &TerminalProperties{
		Phone:                   in.Header.PhoneNumber,
		TerminalType:            in.TerminalType,
		ManufacturerID:          in.ManufacturerID,
		TerminalModel:           in.TerminalModel,
		TerminalID:              in.TerminalID,
		ICCID:                   in.ICCID,
		HardwareVersion:         in.HardwareVersion,
		FirmwareVersion:         in.FirmwareVersion,
		GNSSProperties:          in.GNSSProperties,
		CommunicationProperties: in.CommunicationProperties,
		UpdatedAt:               time.Now(),
	}
}
