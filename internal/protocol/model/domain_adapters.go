package model

import (
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/domain"
)

// NewDevice 由 0x0100 注册报文与会话构造领域设备档案。
func NewDevice(in *Msg0100, session *Session) *domain.Device {
	return &domain.Device{
		ID:              in.DeviceID,
		Plate:           in.PlateNumber,
		Phone:           in.Header.PhoneNumber,
		SessionID:       session.ID,
		TransProto:      session.GetTransProto(),
		Conn:            session.Conn,
		Keepalive:       time.Minute * 1,
		LastestComTime:  time.Now(),
		Status:          domain.DeviceStatusOffline,
		VersionDesc:     in.Header.Attr.VersionDesc,
		ProtocolVersion: in.Header.ProtocolVersion,
	}
}

// NewTerminalProperties 由 0x0107 应答构造属性档案。
func NewTerminalProperties(in *Msg0107) *domain.TerminalProperties {
	return &domain.TerminalProperties{
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
