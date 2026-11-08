package repository

import (
	"fmt"
	"strings"
	"time"
)

// HistoryQuery 是定位/报警历史的分页查询条件。
//
// 时间区间为半开区间 [From, To)；From/To 为零值时表示该侧不限。
// 定位历史按终端上报时间（report_time_ms）过滤，报警历史按服务端接收时间
// （received_at_ms）过滤——两者都基于现有索引，避免管理面查询退化为全表扫描。
// Offset 必须非负，Limit 必须为正且由调用方限制上限。
type HistoryQuery struct {
	Phone  string
	From   time.Time
	To     time.Time
	Offset int
	Limit  int
}

// Validate 校验分页参数与时间区间，防止仓储层执行无上限查询。
func (q HistoryQuery) Validate() error {
	if strings.TrimSpace(q.Phone) == "" {
		return fmt.Errorf("history query phone must not be empty")
	}
	if q.Offset < 0 {
		return fmt.Errorf("history query offset must be >= 0, got %d", q.Offset)
	}
	if q.Limit <= 0 {
		return fmt.Errorf("history query limit must be > 0, got %d", q.Limit)
	}
	if !q.From.IsZero() && !q.To.IsZero() && !q.From.Before(q.To) {
		return fmt.Errorf("history query from must be earlier than to")
	}
	return nil
}
