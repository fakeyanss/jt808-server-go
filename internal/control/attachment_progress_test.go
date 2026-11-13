package control

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func TestAttachmentProgressStoreRoundTripAndUpsert(t *testing.T) {
	files := memory.NewAttachmentFileRepository()
	clk := fixedClock{now: time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)}
	store := NewAttachmentProgressStore(files, clk)
	require.NotNil(t, store)

	progress := contracts.AttachmentProgress{
		Source:    storage.SourceSubia,
		Phone:     "13800138000",
		AlarmNo:   "A1",
		FileName:  "clip.bin",
		TotalSize: 8,
		Segments: []contracts.AttachmentProgressSegment{
			{Index: 0, Key: "k0", Offset: 0, Size: 4, State: "pending"},
		},
		Status:    contracts.AttachmentStatusReceiving,
		CreatedAt: clk.now,
	}
	require.NoError(t, store.Save(context.Background(), progress))

	loaded, err := store.Load(context.Background(), progress.Key())
	require.NoError(t, err)
	require.EqualValues(t, 8, loaded.TotalSize)
	require.Len(t, loaded.Segments, 1)

	// 同一业务键重复上报是 upsert，不新增行。
	progress.Status = contracts.AttachmentStatusCompleted
	progress.Watermark = 8
	progress.ReceivedBytes = 8
	require.NoError(t, store.Save(context.Background(), progress))
	rows, err := files.ListByPhone(context.Background(), storage.SourceSubia, progress.Phone, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, repository.AttachmentStatusCompleted, rows[0].Status)

	_, err = store.Load(context.Background(), contracts.AttachmentProgressKey{
		Source: storage.SourceSubia, Phone: "13800138000", AlarmNo: "A1", FileName: "missing.bin",
	})
	require.ErrorIs(t, err, contracts.ErrAttachmentProgressNotFound)
}

// 上报全部丢失时，control 仅凭对象 key 就能重建完成状态。
func TestAttachmentMaintenanceBackfillsFromObjectKeys(t *testing.T) {
	root := t.TempDir()
	volume, err := storage.NewSharedVolume(filepath.Join(root, "blob"), false)
	require.NoError(t, err)

	total := int64(12)
	for index, offset := range []int64{0, 4, 8} {
		key := storage.AttachmentSegmentObjectKey(storage.SourceSubia, "13800138000", "A1", "clip.bin", offset, 4, total, index)
		info, putErr := volume.Put(context.Background(), key, bytes.NewReader(bytes.Repeat([]byte{byte(index)}, 4)), contracts.BlobOptions{})
		require.NoError(t, putErr)
		_, putErr = volume.Promote(context.Background(), info)
		require.NoError(t, putErr)
	}

	files := memory.NewAttachmentFileRepository()
	maint := NewAttachmentMaintenance(files, volume,
		AttachmentMaintenanceOptions{SessionTimeout: time.Hour, Retention: time.Hour}, fixedClock{now: time.Now()})
	require.NotNil(t, maint)

	saved, err := maint.Backfill(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, saved)

	row, err := files.Get(context.Background(), repository.AttachmentFileKey{
		Source: storage.SourceSubia, Phone: "13800138000", AlarmNo: "A1", FileName: "clip.bin",
	})
	require.NoError(t, err)
	require.Equal(t, repository.AttachmentStatusCompleted, row.Status)
	require.EqualValues(t, total, row.TotalSize)
	require.EqualValues(t, total, row.Watermark)
	require.Len(t, row.Segments, 3)
}

// 只有部分切片时扫描结果必须是 receiving，不能因为“已扫描到的段恰好连续”误判完成。
func TestAttachmentMaintenanceBackfillKeepsPartialFileReceiving(t *testing.T) {
	root := t.TempDir()
	volume, err := storage.NewSharedVolume(filepath.Join(root, "blob"), false)
	require.NoError(t, err)
	key := storage.AttachmentSegmentObjectKey(storage.SourceSubia, "13800138000", "A1", "clip.bin", 0, 4, 12, 0)
	info, err := volume.Put(context.Background(), key, bytes.NewReader([]byte("abcd")), contracts.BlobOptions{})
	require.NoError(t, err)
	_, err = volume.Promote(context.Background(), info)
	require.NoError(t, err)

	files := memory.NewAttachmentFileRepository()
	maint := NewAttachmentMaintenance(files, volume, AttachmentMaintenanceOptions{}, fixedClock{now: time.Now()})
	saved, err := maint.Backfill(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, saved)

	row, err := files.Get(context.Background(), repository.AttachmentFileKey{
		Source: storage.SourceSubia, Phone: "13800138000", AlarmNo: "A1", FileName: "clip.bin",
	})
	require.NoError(t, err)
	require.Equal(t, repository.AttachmentStatusReceiving, row.Status)
	require.EqualValues(t, 12, row.TotalSize)
	require.EqualValues(t, 4, row.Watermark)
}

func TestAttachmentMaintenanceExpiresStaleReceiving(t *testing.T) {
	root := t.TempDir()
	volume, err := storage.NewSharedVolume(filepath.Join(root, "blob"), false)
	require.NoError(t, err)
	files := memory.NewAttachmentFileRepository()
	record := &repository.AttachmentFile{
		Source: storage.SourceSubia, Phone: "13800138000", AlarmNo: "A1", FileName: "clip.bin",
		TotalSize: 8, Status: repository.AttachmentStatusReceiving,
		StagingPath: "/edge/staging/clip.part", CreatedAt: time.Now(),
	}
	_, err = files.Save(context.Background(), record)
	require.NoError(t, err)

	maint := NewAttachmentMaintenance(files, volume,
		AttachmentMaintenanceOptions{SessionTimeout: time.Hour}, fixedClock{now: time.Now().Add(2 * time.Hour)})
	require.NotNil(t, maint)
	expired, err := maint.ExpireReceiving(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, expired)

	row, err := files.Get(context.Background(), record.Key())
	require.NoError(t, err)
	require.Equal(t, repository.AttachmentStatusExpired, row.Status)
	require.Empty(t, row.StagingPath, "control 不得保留对边缘节点本地路径的引用")
}
