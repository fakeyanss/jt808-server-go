package retention

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/mediaupload"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// fakeRetentionStore 用切片模拟按时间分批删除的 SQL 实现。
type fakeRetentionStore struct {
	locations []int64
	alarms    []int64
	files     []repository.MediaFile
	captures  []int64

	purgeErr  map[string]error
	listErr   error
	deleteErr error

	purgeCalls int
}

func (f *fakeRetentionStore) PurgeLocations(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	if err := f.purgeErr["locations"]; err != nil {
		return 0, err
	}
	f.purgeCalls++
	var removed int64
	for removed < int64(limit) && len(f.locations) > 0 && f.locations[0] < cutoff.UnixMilli() {
		f.locations = f.locations[1:]
		removed++
	}
	return removed, nil
}

func (f *fakeRetentionStore) PurgeAlarms(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	if err := f.purgeErr["alarms"]; err != nil {
		return 0, err
	}
	var removed int64
	for removed < int64(limit) && len(f.alarms) > 0 && f.alarms[0] < cutoff.UnixMilli() {
		f.alarms = f.alarms[1:]
		removed++
	}
	return removed, nil
}

func (f *fakeRetentionStore) ListExpiredMediaFiles(_ context.Context, cutoff time.Time, limit int) ([]repository.MediaFile, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	result := make([]repository.MediaFile, 0, limit)
	for _, file := range f.files {
		if len(result) >= limit {
			break
		}
		if file.ReceivedAt.Before(cutoff) {
			result = append(result, file)
		}
	}
	return result, nil
}

func (f *fakeRetentionStore) DeleteMediaFiles(_ context.Context, files []repository.MediaFile) (int64, error) {
	if f.deleteErr != nil {
		return 0, f.deleteErr
	}
	keys := make(map[string]struct{}, len(files))
	for _, file := range files {
		keys[mediaKey(file.Phone, file.MediaID)] = struct{}{}
	}
	kept := make([]repository.MediaFile, 0, len(f.files))
	var deleted int64
	for _, file := range f.files {
		if _, ok := keys[mediaKey(file.Phone, file.MediaID)]; ok {
			deleted++
			continue
		}
		kept = append(kept, file)
	}
	f.files = kept
	return deleted, nil
}

func (f *fakeRetentionStore) PurgeMediaCaptures(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	if err := f.purgeErr["mediaCaptures"]; err != nil {
		return 0, err
	}
	var removed int64
	for removed < int64(limit) && len(f.captures) > 0 && f.captures[0] < cutoff.UnixMilli() {
		f.captures = f.captures[1:]
		removed++
	}
	return removed, nil
}

func mediaKey(phone string, mediaID uint32) string {
	return phone + "/" + strconv.FormatUint(uint64(mediaID), 10)
}

func testNow() time.Time { return time.UnixMilli(1_700_000_000_000) }

// 未开启任何类别时不删除数据，符合“默认不做破坏性清理”。
func TestCleanerDisabledCategoriesKeepData(t *testing.T) {
	store := &fakeRetentionStore{
		locations: []int64{testNow().UnixMilli() - 100*24*3600*1000},
		alarms:    []int64{testNow().UnixMilli() - 100*24*3600*1000},
		captures:  []int64{testNow().UnixMilli() - 100*24*3600*1000},
	}
	metrics := observability.NewMetrics()
	cleaner := NewCleaner(store, nil, Config{}, metrics)
	cleaner.now = testNow

	summary := cleaner.CleanOnce(context.Background())
	assert.Zero(t, summary.Deleted())
	assert.Len(t, store.locations, 1)
	assert.Len(t, store.alarms, 1)
	assert.Len(t, store.captures, 1)
	// 即使没有可清理的数据也记录本次运行时间，便于确认调度在跑。
	assert.NotZero(t, metrics.RetentionSnapshot().LastRunUnixMilli)
	assert.Zero(t, metrics.RetentionSnapshot().Deleted[observability.RetentionCategoryLocations])
}

// 按时间边界删除：早于 cutoff 删除，等于 cutoff 保留。
func TestCleanerTimeBoundary(t *testing.T) {
	now := testNow()
	store := &fakeRetentionStore{
		locations: []int64{
			now.Add(-2 * time.Hour).UnixMilli(),
			now.Add(-time.Hour).UnixMilli(), // 等于 cutoff（1 小时），保留
			now.UnixMilli(),
		},
		alarms: []int64{
			now.Add(-25 * time.Hour).UnixMilli(),
			now.Add(-24 * time.Hour).UnixMilli(), // 等于 cutoff（24 小时），保留
		},
	}
	metrics := observability.NewMetrics()
	cleaner := NewCleaner(store, nil, Config{Locations: time.Hour, Alarms: 24 * time.Hour}, metrics)
	cleaner.now = testNow

	summary := cleaner.CleanOnce(context.Background())
	assert.EqualValues(t, 1, summary.Locations)
	assert.EqualValues(t, 1, summary.Alarms)
	assert.Zero(t, summary.Failures)
	assert.Equal(t, now.Add(-time.Hour).UnixMilli(), store.locations[0])
	assert.Equal(t, now.Add(-24*time.Hour).UnixMilli(), store.alarms[0])

	snapshot := metrics.RetentionSnapshot()
	assert.EqualValues(t, 1, snapshot.Deleted[observability.RetentionCategoryLocations])
	assert.Equal(t, now.UnixMilli(), snapshot.LastRunUnixMilli)
}

// 超过一批的数据按批次循环删除，直到不足一批。
func TestCleanerDeletesInBatches(t *testing.T) {
	now := testNow()
	locations := make([]int64, 0, 6)
	for i := 0; i < 5; i++ {
		locations = append(locations, now.Add(-2*time.Hour-time.Duration(i)*time.Minute).UnixMilli())
	}
	locations = append(locations, now.UnixMilli())
	sort.Slice(locations, func(i, j int) bool { return locations[i] < locations[j] })
	store := &fakeRetentionStore{locations: locations}
	cleaner := NewCleaner(store, nil, Config{Locations: time.Hour, BatchSize: 2}, observability.NewMetrics())
	cleaner.now = testNow

	summary := cleaner.CleanOnce(context.Background())
	assert.EqualValues(t, 5, summary.Locations)
	assert.Len(t, store.locations, 1)
	assert.GreaterOrEqual(t, store.purgeCalls, 3)
}

// 某个类别失败不影响其他类别，并计入失败指标。
func TestCleanerCategoryFailureIsIsolated(t *testing.T) {
	now := testNow()
	store := &fakeRetentionStore{
		locations: []int64{now.Add(-2 * time.Hour).UnixMilli()},
		alarms:    []int64{now.Add(-2 * time.Hour).UnixMilli()},
		purgeErr:  map[string]error{"alarms": errors.New("boom")},
	}
	metrics := observability.NewMetrics()
	cleaner := NewCleaner(store, nil, Config{Locations: time.Hour, Alarms: time.Hour}, metrics)
	cleaner.now = testNow

	summary := cleaner.CleanOnce(context.Background())
	assert.EqualValues(t, 1, summary.Locations)
	assert.Zero(t, summary.Alarms)
	assert.Equal(t, 1, summary.Failures)
	assert.Empty(t, store.locations)
	assert.Len(t, store.alarms, 1)
	assert.EqualValues(t, 1, metrics.RetentionSnapshot().Failures[observability.RetentionCategoryAlarms])
}

// 媒体文件先删文件再删元数据；两个过期文件都删除，保留期内文件不动。
func TestCleanerMediaFilesDeletesDiskAndMetadata(t *testing.T) {
	dir := t.TempDir()
	mediaStore, err := mediaupload.NewStore(dir)
	require.NoError(t, err)

	now := testNow()
	expired := []repository.MediaFile{
		{Phone: "188765440001", MediaID: 1, RelativePath: filepath.Join("188765440001", "20231114", "1.jpg"),
			ReceivedAt: now.Add(-48 * time.Hour)},
		{Phone: "188765440001", MediaID: 2, RelativePath: filepath.Join("188765440001", "20231114", "2.jpg"),
			ReceivedAt: now.Add(-25 * time.Hour)},
	}
	kept := repository.MediaFile{Phone: "188765440001", MediaID: 3,
		RelativePath: filepath.Join("188765440001", "20231114", "3.jpg"), ReceivedAt: now}
	for _, file := range []repository.MediaFile{expired[0], expired[1], kept} {
		path := filepath.Join(dir, file.RelativePath)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte("data"), 0o644))
	}

	store := &fakeRetentionStore{files: []repository.MediaFile{expired[0], expired[1], kept}}
	cleaner := NewCleaner(store, mediaStore, Config{MediaFiles: 24 * time.Hour}, observability.NewMetrics())
	cleaner.now = testNow

	summary := cleaner.CleanOnce(context.Background())
	assert.EqualValues(t, 2, summary.MediaFiles)
	assert.Zero(t, summary.Failures)
	require.Len(t, store.files, 1)
	assert.Equal(t, uint32(3), store.files[0].MediaID)
	for _, file := range expired {
		_, statErr := os.Stat(filepath.Join(dir, file.RelativePath))
		assert.True(t, os.IsNotExist(statErr), "expired media file should be removed: %s", file.RelativePath)
	}
	_, statErr := os.Stat(filepath.Join(dir, kept.RelativePath))
	assert.NoError(t, statErr)
}

// 文件删除失败时保留元数据，等待下次运行重试，不产生“库删了文件还在”。
func TestCleanerMediaFileRemovalFailureKeepsMetadata(t *testing.T) {
	dir := t.TempDir()
	mediaStore, err := mediaupload.NewStore(dir)
	require.NoError(t, err)

	now := testNow()
	file := repository.MediaFile{Phone: "188765440002", MediaID: 10,
		RelativePath: filepath.Join("188765440002", "20231114", "10.jpg"), ReceivedAt: now.Add(-48 * time.Hour)}

	// 在目标路径上放一个非空目录，os.Remove 会失败。
	target := filepath.Join(dir, file.RelativePath)
	require.NoError(t, os.MkdirAll(target, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "blocker"), []byte("x"), 0o644))

	store := &fakeRetentionStore{files: []repository.MediaFile{file}}
	metrics := observability.NewMetrics()
	cleaner := NewCleaner(store, mediaStore, Config{MediaFiles: 24 * time.Hour}, metrics)
	cleaner.now = testNow

	summary := cleaner.CleanOnce(context.Background())
	assert.Zero(t, summary.MediaFiles)
	assert.GreaterOrEqual(t, summary.Failures, 1)
	// 元数据保留，下次清理仍能看到这条记录并重试。
	require.Len(t, store.files, 1)
	assert.EqualValues(t, 1, metrics.RetentionSnapshot().Failures[observability.RetentionCategoryMediaFiles])
}

// 文件已不存在视为删除成功，元数据继续删除，保证崩溃后能自愈。
func TestCleanerMediaFileMissingStillDeletesMetadata(t *testing.T) {
	dir := t.TempDir()
	mediaStore, err := mediaupload.NewStore(dir)
	require.NoError(t, err)

	now := testNow()
	file := repository.MediaFile{Phone: "188765440003", MediaID: 11,
		RelativePath: filepath.Join("188765440003", "20231114", "11.jpg"), ReceivedAt: now.Add(-48 * time.Hour)}
	store := &fakeRetentionStore{files: []repository.MediaFile{file}}
	cleaner := NewCleaner(store, mediaStore, Config{MediaFiles: 24 * time.Hour}, observability.NewMetrics())
	cleaner.now = testNow

	summary := cleaner.CleanOnce(context.Background())
	assert.EqualValues(t, 1, summary.MediaFiles)
	assert.Zero(t, summary.Failures)
	assert.Empty(t, store.files)
}

// 媒体清理开启但未注入媒体仓储时按类别失败处理，不影响其他类别。
func TestCleanerMediaFilesWithoutStoreFails(t *testing.T) {
	now := testNow()
	store := &fakeRetentionStore{
		files: []repository.MediaFile{{Phone: "188765440004", MediaID: 12, RelativePath: "x",
			ReceivedAt: now.Add(-48 * time.Hour)}},
		locations: []int64{now.Add(-2 * time.Hour).UnixMilli()},
	}
	cleaner := NewCleaner(store, nil, Config{Locations: time.Hour, MediaFiles: time.Hour}, observability.NewMetrics())
	cleaner.now = testNow

	summary := cleaner.CleanOnce(context.Background())
	assert.EqualValues(t, 1, summary.Locations)
	assert.Zero(t, summary.MediaFiles)
	assert.Equal(t, 1, summary.Failures)
	assert.Len(t, store.files, 1)
}

// 不支持清理的后端（store=nil）直接跳过，不误删也不 panic。
func TestCleanerNilStoreSkips(t *testing.T) {
	cleaner := NewCleaner(nil, nil, Config{Locations: time.Hour}, observability.NewMetrics())
	summary := cleaner.CleanOnce(context.Background())
	assert.Zero(t, summary.Deleted())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := cleaner.Done()
	cleaner.Run(ctx)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleaner Run should exit immediately when store is nil")
	}
}

// 批间等待可通过取消 ctx 提前结束，避免停机被拖住。
func TestCleanerStopsWhenContextCancelled(t *testing.T) {
	now := testNow()
	store := &fakeRetentionStore{}
	for i := 0; i < 10; i++ {
		store.locations = append(store.locations, now.Add(-2*time.Hour).UnixMilli())
	}
	cleaner := NewCleaner(store, nil, Config{Locations: time.Hour, BatchSize: 1}, observability.NewMetrics())
	cleaner.now = testNow

	ctx, cancel := context.WithCancel(context.Background())
	// 取消后仍允许完成当前批次，但不再继续下一批。
	cancel()
	summary := cleaner.CleanOnce(ctx)
	assert.LessOrEqual(t, summary.Locations, int64(1))
	assert.NotZero(t, len(store.locations))
}

func TestSummaryDeleted(t *testing.T) {
	assert.EqualValues(t, 10, (Summary{Locations: 1, Alarms: 2, MediaFiles: 3, MediaCaptures: 4}).Deleted())
	assert.Zero(t, (Summary{}).Deleted())
}

func TestConfigNormalized(t *testing.T) {
	cfg := Config{}.normalized()
	assert.Equal(t, time.Hour, cfg.Interval)
	assert.Equal(t, 1000, cfg.BatchSize)
	assert.Zero(t, cfg.Locations)
}
