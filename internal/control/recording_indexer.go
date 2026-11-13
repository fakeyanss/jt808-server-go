package control

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

// RecordingIndexer 从对象存储重建平台录像切片索引。
//
// 定位：对象存储是切片的事实来源，recording_segments 只是可重建的目录。
// media 的上报允许丢失；本索引器扫描 media/segments/ 前缀，从对象 key
// 解析终端、通道、起止时间与序号，并以幂等 upsert 补齐缺失行。
// 它只增不删，过期切片仍由保留期清理负责。
type RecordingIndexer struct {
	segments repository.RecordingSegmentRepository
	blob     contracts.BlobStore
}

// NewRecordingIndexer 构造切片索引补齐器；segments 或 blob 为空时禁用。
func NewRecordingIndexer(segments repository.RecordingSegmentRepository, blob contracts.BlobStore) *RecordingIndexer {
	if segments == nil || blob == nil {
		return nil
	}
	return &RecordingIndexer{segments: segments, blob: blob}
}

// Backfill 扫描一次 media/segments/ 并补齐索引，返回新增/更新的行数。
func (i *RecordingIndexer) Backfill(ctx context.Context) (int, error) {
	if i == nil {
		return 0, nil
	}
	lister, ok := i.blob.(contracts.BlobLister)
	if !ok {
		return 0, fmt.Errorf("blob backend %T does not support listing", i.blob)
	}
	objects, err := lister.ListObjects(ctx, storage.MediaSegments)
	if err != nil {
		return 0, fmt.Errorf("list media segments: %w", err)
	}
	saved := 0
	for _, object := range objects {
		info, parseErr := storage.ParseMediaSegmentKey(object.Key)
		if parseErr != nil {
			log.Warn().Err(parseErr).Str("key", object.Key).
				Msg("Skip media segment object with invalid key metadata")
			continue
		}
		segment := &repository.RecordingSegment{
			ID:        fmt.Sprintf("%s_%d_%d", info.Phone, info.Channel, info.StartAt.UnixMilli()),
			Phone:     info.Phone,
			Channel:   info.Channel,
			Kind:      info.Kind,
			ObjectKey: info.ObjectKey,
			StartAt:   info.StartAt,
			EndAt:     info.EndAt,
			Size:      object.Size,
			Keyframes: []byte("[]"),
			State:     repository.RecordingSegmentStateCommitted,
			CreatedAt: info.StartAt,
		}
		if saveErr := i.segments.Save(ctx, segment); saveErr != nil {
			log.Warn().Err(saveErr).Str("key", object.Key).
				Msg("Fail to backfill media segment index")
			continue
		}
		saved++
	}
	if saved > 0 {
		log.Info().Int("objects", len(objects)).Int("saved", saved).
			Msg("Media segment index backfilled from object storage")
	}
	return saved, nil
}

// DefaultRecordingIndexInterval 是索引补齐的默认周期。
const DefaultRecordingIndexInterval = 10 * time.Minute
