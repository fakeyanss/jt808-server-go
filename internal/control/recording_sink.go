package control

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/fakeyanss/jt808-server-go/internal/media"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// SegmentRepoSink 把 media 的切片完成事件直接写入 control 的索引仓储。
//
// 仅用于 media 与 control 同进程（standalone 或合并角色）的部署；
// 边缘 media 节点不得持有仓储，应使用 HTTP 出站上报。
type SegmentRepoSink struct {
	segments repository.RecordingSegmentRepository
}

// NewSegmentRepoSink 构造同进程切片索引接收器。
func NewSegmentRepoSink(segments repository.RecordingSegmentRepository) *SegmentRepoSink {
	return &SegmentRepoSink{segments: segments}
}

// OnSegment 幂等 upsert 一片切片索引。
func (s *SegmentRepoSink) OnSegment(ctx context.Context, info media.SegmentInfo) error {
	if s == nil || s.segments == nil {
		return nil
	}
	return s.segments.Save(ctx, RecordingSegmentFromInfo(info))
}

// RecordingSegmentFromInfo 把录制侧切片信息转换成索引行。
func RecordingSegmentFromInfo(info media.SegmentInfo) *repository.RecordingSegment {
	keyframes, err := json.Marshal(info.Keyframes)
	if err != nil {
		keyframes = []byte("[]")
	}
	state := repository.RecordingSegmentStateLocal
	if info.State == "committed" {
		state = repository.RecordingSegmentStateCommitted
	}
	return &repository.RecordingSegment{
		ID:        fmt.Sprintf("%s_%d_%d", info.Phone, info.Channel, info.StartTime.UnixMilli()),
		Phone:     info.Phone,
		Channel:   info.Channel,
		Kind:      string(info.Kind),
		ObjectKey: info.ObjectKey,
		LocalPath: info.LocalPath,
		StartAt:   info.StartTime,
		EndAt:     info.EndTime,
		Size:      info.Size,
		Keyframes: keyframes,
		State:     state,
		CreatedAt: info.StartTime,
	}
}

var _ media.SegmentSink = (*SegmentRepoSink)(nil)
