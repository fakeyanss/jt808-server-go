package control

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

func promoteObject(t *testing.T, volume contracts.BlobStore, key string, payload []byte) {
	t.Helper()
	info, err := volume.Put(context.Background(), key, bytes.NewReader(payload), contracts.BlobOptions{})
	require.NoError(t, err)
	_, err = volume.Promote(context.Background(), info)
	require.NoError(t, err)
}

func writeFTPManifest(t *testing.T, volume contracts.BlobStore, phone, serial, file string, manifest storage.AttachmentDoneManifest) string {
	t.Helper()
	payload, err := json.Marshal(manifest)
	require.NoError(t, err)
	key := storage.AttachmentDoneObjectKey(storage.SourceFTP1078, phone, serial, file)
	promoteObject(t, volume, key, payload)
	return key
}

// 清单存在且切片连续：扫描必须判完成，总长取自清单。
func TestFTPBackfillCompletesOnlyWhenManifestAndSegmentsAgree(t *testing.T) {
	volume, err := storage.NewSharedVolume(filepath.Join(t.TempDir(), "blob"), false)
	require.NoError(t, err)
	for index, offset := range []int64{0, 4} {
		key := storage.AttachmentFTPSegmentObjectKey("13800138000", "9", "alarm.mp4", offset, 4, index)
		promoteObject(t, volume, key, bytes.Repeat([]byte("a"), 4))
	}
	writeFTPManifest(t, volume, "13800138000", "9", "alarm.mp4", storage.AttachmentDoneManifest{
		FileName: "alarm.mp4", TotalSize: 8, SegmentSize: 4, Segments: 2, CompletedAt: time.Now().UnixMilli(),
	})

	files := memory.NewAttachmentFileRepository()
	maint := NewAttachmentMaintenance(files, volume,
		AttachmentMaintenanceOptions{Metrics: observability.NewMetrics()}, fixedClock{now: time.Now()})
	_, err = maint.Backfill(context.Background())
	require.NoError(t, err)

	row, err := files.Get(context.Background(), repository.AttachmentFileKey{
		Source: storage.SourceFTP1078, Phone: "13800138000", AlarmNo: "9", FileName: "alarm.mp4",
	})
	require.NoError(t, err)
	require.Equal(t, repository.AttachmentStatusCompleted, row.Status)
	require.EqualValues(t, 8, row.TotalSize)
	require.EqualValues(t, 8, row.Watermark)
}

// 清单存在但中间缺片：不能判完成，必须计数异常并保持 receiving。
func TestFTPBackfillTreatsManifestWithGapAsAnomaly(t *testing.T) {
	volume, err := storage.NewSharedVolume(filepath.Join(t.TempDir(), "blob"), false)
	require.NoError(t, err)
	promoteObject(t, volume,
		storage.AttachmentFTPSegmentObjectKey("13800138000", "9", "alarm.mp4", 0, 4, 0),
		bytes.Repeat([]byte("a"), 4))
	promoteObject(t, volume,
		storage.AttachmentFTPSegmentObjectKey("13800138000", "9", "alarm.mp4", 8, 4, 2),
		bytes.Repeat([]byte("b"), 4))
	writeFTPManifest(t, volume, "13800138000", "9", "alarm.mp4", storage.AttachmentDoneManifest{
		FileName: "alarm.mp4", TotalSize: 12, SegmentSize: 4, Segments: 3,
	})

	files := memory.NewAttachmentFileRepository()
	metrics := observability.NewMetrics()
	maint := NewAttachmentMaintenance(files, volume,
		AttachmentMaintenanceOptions{Metrics: metrics}, fixedClock{now: time.Now()})
	_, err = maint.Backfill(context.Background())
	require.NoError(t, err)

	row, err := files.Get(context.Background(), repository.AttachmentFileKey{
		Source: storage.SourceFTP1078, Phone: "13800138000", AlarmNo: "9", FileName: "alarm.mp4",
	})
	require.NoError(t, err)
	require.Equal(t, repository.AttachmentStatusReceiving, row.Status)
	require.EqualValues(t, 4, row.Watermark)
	require.EqualValues(t, 1, metrics.FTP1078DoneAnomaly())
}

// 保留期清理必须把 .done 清单与切片一并删除。
func TestFTPRetentionDeletesManifestObject(t *testing.T) {
	volume, err := storage.NewSharedVolume(filepath.Join(t.TempDir(), "blob"), false)
	require.NoError(t, err)
	segmentKey := storage.AttachmentFTPSegmentObjectKey("13800138000", "9", "alarm.mp4", 0, 4, 0)
	promoteObject(t, volume, segmentKey, bytes.Repeat([]byte("a"), 4))
	doneKey := writeFTPManifest(t, volume, "13800138000", "9", "alarm.mp4", storage.AttachmentDoneManifest{
		FileName: "alarm.mp4", TotalSize: 4, SegmentSize: 4, Segments: 1,
	})

	files := memory.NewAttachmentFileRepository()
	completedAt := time.Now().Add(-48 * time.Hour)
	_, err = files.Save(context.Background(), &repository.AttachmentFile{
		Source: storage.SourceFTP1078, Phone: "13800138000", AlarmNo: "9", FileName: "alarm.mp4",
		TotalSize: 4, ReceivedBytes: 4, Watermark: 4, Status: repository.AttachmentStatusCompleted,
		CreatedAt: completedAt, CompletedAt: completedAt,
		Segments: []repository.AttachmentSegment{{Index: 0, Key: segmentKey, Offset: 0, Size: 4}},
	})
	require.NoError(t, err)

	maint := NewAttachmentMaintenance(files, volume,
		AttachmentMaintenanceOptions{Retention: time.Hour}, fixedClock{now: time.Now()})
	removed, err := maint.PruneRetention(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, removed)

	_, err = volume.Stat(context.Background(), doneKey)
	require.Error(t, err, "manifest must be deleted with the uploaded file")
	_, err = volume.Stat(context.Background(), segmentKey)
	require.Error(t, err)
}
