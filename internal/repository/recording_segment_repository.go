package repository

import (
	"context"
	"errors"
	"time"
)

// ErrRecordingSegmentNotFound 表示平台录像切片索引不存在。
var ErrRecordingSegmentNotFound = errors.New("recording segment not found")

// 平台录像切片的存储状态。
const (
	// RecordingSegmentStateLocal 表示切片只在媒体节点本地盘，未上传对象存储。
	RecordingSegmentStateLocal = "local"
	// RecordingSegmentStateCommitted 表示切片已提级并标记 committed。
	RecordingSegmentStateCommitted = "committed"
)

// RecordingSegment 是一片实时流录像的索引。
//
// 它回答三个问题：平台存储里有哪些文件（ObjectKey/LocalPath）、
// 覆盖哪段时间（StartAt/EndAt）、属于哪个终端和通道（Phone/Channel）。
// ObjectKey 与 LocalPath 至少有一个非空；对象存储模式只填 ObjectKey。
type RecordingSegment struct {
	// ID 是切片稳定标识：<phone>_<channel>_<startMillis>。
	ID        string    `json:"id"`
	Phone     string    `json:"phone"`
	Channel   uint8     `json:"channel"`
	Kind      string    `json:"kind"`
	ObjectKey string    `json:"objectKey,omitempty"`
	LocalPath string    `json:"localPath,omitempty"`
	StartAt   time.Time `json:"startAt"`
	EndAt     time.Time `json:"endAt"`
	Size      int64     `json:"size"`
	// Keyframes 是切片内关键帧索引（JSON 数组），供后续回放对齐使用。
	Keyframes []byte    `json:"keyframes,omitempty"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"createdAt"`
}

// Duration 返回切片覆盖时长。
func (s *RecordingSegment) Duration() time.Duration {
	if s == nil || !s.EndAt.After(s.StartAt) {
		return 0
	}
	return s.EndAt.Sub(s.StartAt)
}

// RecordingSegmentRepository 是平台录像切片索引仓储。
//
// 时间字段一律由调用方传入，仓储不读取系统时钟；
// ListByRange 使用半开区间 [start, end)，与播放覆盖查询语义一致。
type RecordingSegmentRepository interface {
	// Save 按 ID upsert 一条切片索引。
	Save(ctx context.Context, segment *RecordingSegment) error
	// Get 按终端与 ID 读取切片；不存在返回 ErrRecordingSegmentNotFound。
	Get(ctx context.Context, phone, id string) (*RecordingSegment, error)
	// ListByRange 返回与 [start, end) 有交集的切片，按开始时间升序。
	ListByRange(ctx context.Context, phone string, channel uint8, start, end time.Time) ([]*RecordingSegment, error)
	// ListExpired 返回结束时间早于 cutoff 的切片，供保留期清理。
	ListExpired(ctx context.Context, cutoff time.Time, limit int) ([]*RecordingSegment, error)
	// Delete 按 ID 批量删除索引，返回实际删除行数。
	Delete(ctx context.Context, ids []string) (int64, error)
	// ReferencedSegment 判断对象 key 是否仍被切片索引引用。
	ReferencedSegment(ctx context.Context, key string) (bool, error)
	// Stats 返回切片总数与总占用字节数。
	Stats(ctx context.Context) (count int64, bytes int64, err error)
}
