package repository

import (
	"context"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// LocationRecord 是一条待持久化的定位历史，来源为 0x0200 或 0x0704 数据项。
type LocationRecord struct {
	Phone      string                  `json:"phone"`
	MsgID      uint16                  `json:"msgId"`
	AlarmSign  uint32                  `json:"alarmSign"`
	StatusSign uint32                  `json:"statusSign"`
	Latitude   uint32                  `json:"latitude"`
	Longitude  uint32                  `json:"longitude"`
	Altitude   uint16                  `json:"altitude"`
	Speed      uint16                  `json:"speed"`
	Direction  uint16                  `json:"direction"`
	ExtItems   []model.LocationExtItem `json:"extItems,omitempty"`
	OccurredAt string                  `json:"occurredAt"`
	ReceivedAt time.Time               `json:"receivedAt"`
}

// LocationRepository 定位历史仓储（B 类）。
// 与 GeoRepository 的热读环形缓冲不同，本仓储面向持久化历史与保留期清理。
// SQL 后端按 LocationRetention 执行时间清理与每设备条数上限；memory 后端不启用。
type LocationRepository interface {
	// Record 批量写入定位记录。SQL 后端按设备节流执行保留期清理。
	Record(ctx context.Context, records []LocationRecord) error
	// List 返回设备定位历史，按接收时间倒序；limit <= 0 表示不限制条数。
	List(ctx context.Context, phone string, limit int) ([]LocationRecord, error)
	// ListPage 返回设备在时间区间内的定位历史与总条数，按上报时间倒序。
	// 用于管理面分页查询，必须同时返回总数以便调用方判断是否还有下一页。
	ListPage(ctx context.Context, query HistoryQuery) ([]LocationRecord, int, error)
}
