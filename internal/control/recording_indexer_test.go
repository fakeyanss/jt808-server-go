package control

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

// 通知全部丢失时，control 仅凭对象 key 就能重建切片索引。
func TestRecordingIndexerBackfillFromObjectKeys(t *testing.T) {
	root := t.TempDir()
	volume, err := storage.NewSharedVolume(root, false)
	require.NoError(t, err)

	start := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	key := storage.MediaSegmentObjectKey("13800138000", 1, "live", start, 10*time.Minute, 0)
	writeSegmentObject(t, root, key, 2048)
	// 一个不合规对象必须被跳过，不能写坏索引。
	writeSegmentObject(t, root, "media/segments/13800138000/ch1/live/20261114/broken.ts", 10)

	segments := memory.NewRecordingSegmentRepository()
	indexer := NewRecordingIndexer(segments, volume)
	require.NotNil(t, indexer)

	saved, err := indexer.Backfill(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, saved)

	rows, err := segments.ListByRange(context.Background(), "13800138000", 1, start, start.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, key, rows[0].ObjectKey)
	require.EqualValues(t, 2048, rows[0].Size)
	require.Equal(t, "live", rows[0].Kind)
	require.True(t, start.Equal(rows[0].StartAt))
	require.Equal(t, 10*time.Minute, rows[0].Duration())
	require.Equal(t, "committed", rows[0].State)

	// 重复扫描是幂等的，不产生新行。
	saved, err = indexer.Backfill(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, saved)
	count, _, err := segments.Stats(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func writeSegmentObject(t *testing.T, root, key string, size int) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(key))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o750))
	require.NoError(t, os.WriteFile(target, make([]byte, size), 0o640))
}
