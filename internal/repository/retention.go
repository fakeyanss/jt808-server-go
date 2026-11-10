package repository

import (
	"context"
	"time"
)

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

// RetentionStore 提供历史数据的分批清理能力，仅 SQL 后端实现。
//
// 约定：
//   - 每个 Purge 方法最多删除 limit 行，调用方循环调用直到返回行数小于 limit；
//   - cutoff 为过期边界，严格早于该时刻的数据才会被删除；
//   - 返回值为本次实际删除的行数（mediaCaptures 只统计主表行数）。
//
// media_files 需要先删文件再删元数据，因此拆成查询与删除两个步骤：
// ListExpiredMediaFiles 只读，DeleteMediaFiles 只删除调用方确认已删除文件的行。
type RetentionStore interface {
	PurgeLocations(ctx context.Context, cutoff time.Time, limit int) (int64, error)
	PurgeAlarms(ctx context.Context, cutoff time.Time, limit int) (int64, error)
	ListExpiredMediaFiles(ctx context.Context, cutoff time.Time, limit int) ([]MediaFile, error)
	DeleteMediaFiles(ctx context.Context, files []MediaFile) (int64, error)
	PurgeMediaCaptures(ctx context.Context, cutoff time.Time, limit int) (int64, error)
}
