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
}

// RegisterEvent 终端注册（0x0100）事件。
type RegisterEvent struct {
	Phone           string `json:"phone"`
	DeviceID        string `json:"deviceId"`
	Plate           string `json:"plate"`
	ProtocolVersion uint8  `json:"protocolVersion"`
	VersionDesc     string `json:"versionDesc"`
	Result          uint8  `json:"result"` // 0x8100 应答结果，0 表示成功
}

// AuthEvent 终端鉴权（0x0102）事件。
type AuthEvent struct {
	Phone           string `json:"phone"`
	IMEI            string `json:"imei"`
	SoftwareVersion string `json:"softwareVersion"`
	Success         bool   `json:"success"`
}

// LocationEvent 位置上报（0x0200 / 0x0704）事件。
type LocationEvent struct {
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
	Phone     string `json:"phone"`
	Serial    uint16 `json:"serial"`
	AlarmSign uint32 `json:"alarmSign"`
	Latitude  uint32 `json:"latitude"`
	Longitude uint32 `json:"longitude"`
	Time      string `json:"time"`
}

// DisconnectEvent 连接断开事件。
type DisconnectEvent struct {
	SessionID string `json:"sessionId"`
	Transport string `json:"transport"`
}

// Noop 是默认钩子实现，所有回调均为空操作。
type Noop struct{}

func (Noop) OnRegister(context.Context, RegisterEvent)     {}
func (Noop) OnAuth(context.Context, AuthEvent)             {}
func (Noop) OnLocation(context.Context, LocationEvent)     {}
func (Noop) OnAlarm(context.Context, AlarmEvent)           {}
func (Noop) OnDisconnect(context.Context, DisconnectEvent) {}
