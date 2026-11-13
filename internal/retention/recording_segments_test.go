package retention

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
)

// 过期切片先删本地内容再删索引；未到期的切片保持不动。
func TestCleanerPurgesExpiredRecordingSegments(t *testing.T) {
	now := testNow()
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.ts")
	newPath := filepath.Join(dir, "new.ts")
	require.NoError(t, os.WriteFile(oldPath, []byte("old"), 0o640))
	require.NoError(t, os.WriteFile(newPath, []byte("new"), 0o640))

	segments := memory.NewRecordingSegmentRepository()
	require.NoError(t, segments.Save(context.Background(), &repository.RecordingSegment{
		ID: "old", Phone: "13800138000", Channel: 1, LocalPath: oldPath,
		StartAt: now.Add(-3 * time.Hour), EndAt: now.Add(-2 * time.Hour),
		Size: 3, State: repository.RecordingSegmentStateLocal,
	}))
	require.NoError(t, segments.Save(context.Background(), &repository.RecordingSegment{
		ID: "new", Phone: "13800138000", Channel: 1, LocalPath: newPath,
		StartAt: now.Add(-30 * time.Minute), EndAt: now.Add(-20 * time.Minute),
		Size: 3, State: repository.RecordingSegmentStateLocal,
	}))

	store := &fakeRetentionStore{}
	cleaner := NewCleaner(store, nil, Config{RecordingSegments: time.Hour}, observability.NewMetrics())
	cleaner.now = testNow
	cleaner.SetSegmentStore(segments, nil)

	summary := cleaner.CleanOnce(context.Background())
	assert.EqualValues(t, 1, summary.RecordingSegments)
	assert.Zero(t, summary.Failures)

	_, err := os.Stat(oldPath)
	assert.True(t, errors.Is(err, os.ErrNotExist))
	if _, err := segments.Get(context.Background(), "13800138000", "old"); !errors.Is(err, repository.ErrRecordingSegmentNotFound) {
		t.Fatalf("expired segment row should be deleted, got %v", err)
	}
	_, err = os.Stat(newPath)
	assert.NoError(t, err)
	if _, err := segments.Get(context.Background(), "13800138000", "new"); err != nil {
		t.Fatalf("fresh segment row should remain: %v", err)
	}
}

// 对象删除失败时保留索引行，下一轮重试。
func TestCleanerKeepsSegmentRowWhenObjectDeleteFails(t *testing.T) {
	now := testNow()
	segments := memory.NewRecordingSegmentRepository()
	require.NoError(t, segments.Save(context.Background(), &repository.RecordingSegment{
		ID: "obj", Phone: "13800138000", Channel: 1,
		ObjectKey: "media/segments/13800138000/ch1/20261114/a.ts",
		StartAt:   now.Add(-3 * time.Hour), EndAt: now.Add(-2 * time.Hour),
		Size: 3, State: repository.RecordingSegmentStateCommitted,
	}))

	cleaner := NewCleaner(&fakeRetentionStore{}, nil, Config{RecordingSegments: time.Hour}, observability.NewMetrics())
	cleaner.now = testNow
	cleaner.SetSegmentStore(segments, nil)

	summary := cleaner.CleanOnce(context.Background())
	assert.Zero(t, summary.RecordingSegments)
	assert.Equal(t, 1, summary.Failures)
	if _, err := segments.Get(context.Background(), "13800138000", "obj"); err != nil {
		t.Fatalf("row must remain when object cannot be deleted: %v", err)
	}
}
