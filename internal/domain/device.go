package domain

import (
	"net"
	"time"
)

type DeviceStatus int8

const (
	DeviceStatusOffline  DeviceStatus = 0
	DeviceStatusOnline   DeviceStatus = 1
	DeviceStatusSleeping DeviceStatus = 2
)

// 终端设备的基础属性信息，用于数据缓存、持久化和保活相关流程处理
type Device struct {
	ID    string `json:"id"` // ID是否可重复？
	Plate string `json:"plate"`
	Phone string `json:"phone"` // 默认通过PhoneNumber来索引设备

	// 连接信息

	SessionID      string            `json:"sessionId"`
	TransProto     TransportProtocol `json:"transProto"`
	Conn           net.Conn          `json:"-"`
	Keepalive      time.Duration     `json:"keepalive"`   // 保活时长
	LastestComTime time.Time         `json:"lastComTime"` // 最近一次交互时间
	Status         DeviceStatus      `json:"status"`

	// 设备信息

	VersionDesc     VersionType `json:"versionDesc"`     // jt808协议版本描述, 区分 2011 / 2013 / 2019
	ProtocolVersion uint8       `json:"protocolVersion"` // jt808协议版本定义, 区分 (2011&2013) / 2019后续版本修订
	AuthCode        string      `json:"authcode"`
	IMEI            string      `json:"imei"`
	SoftwareVersion string      `json:"softwareVersion"` // 终端软件版本号(非jt808协议版本)
}

func (d *Device) ShouleTurnOffline() bool {
	now := time.Now().UnixMilli()
	return d.Status != DeviceStatusOffline && now > d.Keepalive.Milliseconds()+d.LastestComTime.UnixMilli()
}

func (d *Device) ShouldClear() bool {
	now := time.Now().UnixMilli()
	return d.Status == DeviceStatusOffline && now > d.Keepalive.Milliseconds()+d.LastestComTime.UnixMilli()
}
