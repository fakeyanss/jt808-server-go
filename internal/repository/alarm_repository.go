package repository

import (
	"context"
	"time"
)

// 报警保留策略默认值，与历史内存实现保持一致：
// 去重窗口 30 分钟、确认关联 10 分钟、每设备保留 1000 条。
const (
	DefaultAlarmDedupWindow   = 30 * time.Minute
	DefaultAlarmConfirmExpire = 10 * time.Minute
	DefaultAlarmMaxPerDevice  = 1000
)

type AlarmEvent struct {
	Phone         string    `json:"phone"`
	MessageSerial uint16    `json:"messageSerial"`
	AlarmSign     uint32    `json:"alarmSign"`
	Latitude      uint32    `json:"latitude"`
	Longitude     uint32    `json:"longitude"`
	OccurredAt    string    `json:"occurredAt"`
	ReceivedAt    time.Time `json:"receivedAt"`
	Confirmed     bool      `json:"confirmed"`
}

// AlarmRepository 报警历史仓储（B 类）：按设备追加报警事件，并支持
// 报警确认命令下发后的回执关联。SQL 实现下确认命令关联为进程内协调态，
// 重启后未确认的关联失效；已确认结果写入 alarms 表。
type AlarmRepository interface {
	Record(ctx context.Context, phone string, serial uint16, alarmSign, latitude, longitude uint32, occurredAt string) error
	List(ctx context.Context, phone string) ([]AlarmEvent, error)
	// ListPage 返回设备在接收时间区间内的报警历史与总条数，按接收时间倒序。
	// 用于管理面分页查询；memory 与 SQL 后端均实现。
	ListPage(ctx context.Context, query HistoryQuery) ([]AlarmEvent, int, error)
	RegisterConfirmation(ctx context.Context, phone string, commandSerial, alarmSerial uint16, alarmType uint32) error
	ConfirmByCommand(ctx context.Context, phone string, commandSerial uint16) (bool, error)
}
