package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// TestSQLiteRecordingSegmentRepository 校验录像切片索引的范围查询、过期与引用判断。
func TestSQLiteRecordingSegmentRepository(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "jt808.db")
	migrateSQLite(t, dbPath)

	store, err := Open(dbPath, 0)
	require.NoError(t, err)
	defer store.Close()
	segments := store.RecordingSegmentRepository()

	const phone = "188765477110"
	base := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	save := func(id string, channel uint8, start time.Time, length time.Duration, key string) {
		require.NoError(t, segments.Save(ctx, &repository.RecordingSegment{
			ID: id, Phone: phone, Channel: channel, Kind: "live",
			ObjectKey: key, StartAt: start, EndAt: start.Add(length), Size: 1024,
			Keyframes: []byte(`[{"pts":0,"offset":0}]`), State: repository.RecordingSegmentStateCommitted,
			CreatedAt: start,
		}))
	}
	save("seg-1", 1, base, 10*time.Minute, "media/segments/"+phone+"/ch1/20261114/a.ts")
	save("seg-2", 1, base.Add(20*time.Minute), 10*time.Minute, "media/segments/"+phone+"/ch1/20261114/b.ts")
	save("seg-3", 2, base, 10*time.Minute, "media/segments/"+phone+"/ch2/20261114/c.ts")

	got, err := segments.Get(ctx, phone, "seg-1")
	require.NoError(t, err)
	require.Equal(t, uint8(1), got.Channel)
	require.True(t, base.Equal(got.StartAt))
	require.Equal(t, 10*time.Minute, got.Duration())

	if _, err := segments.Get(ctx, phone, "missing"); !errors.Is(err, repository.ErrRecordingSegmentNotFound) {
		t.Fatalf("want not found, got %v", err)
	}

	// 半开区间：与请求区间有交集即返回，不匹配通道的切片不返回。
	items, err := segments.ListByRange(ctx, phone, 1, base.Add(5*time.Minute), base.Add(25*time.Minute))
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, "seg-1", items[0].ID)
	require.Equal(t, "seg-2", items[1].ID)

	items, err = segments.ListByRange(ctx, phone, 1, base.Add(10*time.Minute), base.Add(20*time.Minute))
	require.NoError(t, err)
	require.Empty(t, items)

	expired, err := segments.ListExpired(ctx, base.Add(31*time.Minute), 10)
	require.NoError(t, err)
	require.Len(t, expired, 3)

	referenced, err := segments.ReferencedSegment(ctx, "media/segments/"+phone+"/ch1/20261114/a.ts")
	require.NoError(t, err)
	require.True(t, referenced)
	referenced, err = segments.ReferencedSegment(ctx, "media/segments/"+phone+"/ch1/20261114/unknown.ts")
	require.NoError(t, err)
	require.False(t, referenced)

	count, bytes, err := segments.Stats(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 3, count)
	require.EqualValues(t, 3072, bytes)

	deleted, err := segments.Delete(ctx, []string{"seg-1", "seg-2"})
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)
	items, err = segments.ListByRange(ctx, phone, 1, base, base.Add(time.Hour))
	require.NoError(t, err)
	require.Empty(t, items)
}
