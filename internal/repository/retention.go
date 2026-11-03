package repository

import "time"

// LocationRetention 定义定位历史的保留策略。
// MaxAge <= 0 表示不做时间清理；MaxPerDevice <= 0 表示不限制条数。
type LocationRetention struct {
	MaxAge       time.Duration
	MaxPerDevice int
}

// AlarmRetention 定义报警的去重与保留策略。
// DedupWindow <= 0 表示不去重；ConfirmExpire <= 0 表示确认关联立即失效；
// MaxPerDevice <= 0 表示不限制条数。
type AlarmRetention struct {
	DedupWindow   time.Duration
	ConfirmExpire time.Duration
	MaxPerDevice  int
}
