package retention

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/mediaupload"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
)

// writeMediaFile 在媒体根目录下写入文件，并把 mtime 固定到指定时刻。
func writeMediaFile(t *testing.T, root, relative string, modTime time.Time) string {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(relative))
	require.NoError(t, os.MkdirAll(filepath.Dir(absolute), 0o755))
	require.NoError(t, os.WriteFile(absolute, []byte("data"), 0o644))
	require.NoError(t, os.Chtimes(absolute, modTime, modTime))
	return absolute
}

func newOrphanTestScanner(t *testing.T, store *fakeRetentionStore, cfg OrphanConfig) (*OrphanScanner, *mediaupload.Store, *observability.Metrics) {
	t.Helper()
	dir := t.TempDir()
	mediaStore, err := mediaupload.NewStore(dir)
	require.NoError(t, err)
	metrics := observability.NewMetrics()
	scanner := NewOrphanScanner(store, mediaStore, cfg, metrics)
	scanner.now = testNow
	return scanner, mediaStore, metrics
}

// 进行中的上传（.part 临时文件）与宽限期内的新文件都不能被删除。
func TestOrphanScanSkipsActiveUploadAndRecentFiles(t *testing.T) {
	scanner, mediaStore, metrics := newOrphanTestScanner(t, &fakeRetentionStore{}, OrphanConfig{
		Enabled: true,
		Grace:   7 * 24 * time.Hour,
	})
	old := testNow().Add(-30 * 24 * time.Hour)

	partPath := writeMediaFile(t, mediaStore.Root(), "188765450001/20231114/.1.jpg.123.part", old)
	recentPath := writeMediaFile(t, mediaStore.Root(), "188765450001/20231114/1.jpg", testNow().Add(-time.Hour))
	hiddenPath := writeMediaFile(t, mediaStore.Root(), ".DS_Store", old)

	summary := scanner.ScanOnce(context.Background())
	assert.Zero(t, summary.Deleted)
	assert.Zero(t, summary.Failures)
	assert.GreaterOrEqual(t, summary.Skipped, int64(3))
	assert.GreaterOrEqual(t, summary.Scanned, int64(3))

	for _, path := range []string{partPath, recentPath, hiddenPath} {
		_, err := os.Stat(path)
		assert.NoError(t, err, "file should be kept: %s", path)
	}
	snapshot := metrics.OrphanSnapshot()
	assert.Zero(t, snapshot.Deleted)
	assert.GreaterOrEqual(t, snapshot.Scanned, int64(3))
	assert.Equal(t, testNow().UnixMilli(), snapshot.LastRunUnixMilli)
}

// 补传到旧目录（目录名很旧）但文件 mtime 很新时不能按目录名误删。
func TestOrphanScanKeepsOldDirectoryWithNewFile(t *testing.T) {
	scanner, mediaStore, _ := newOrphanTestScanner(t, &fakeRetentionStore{}, OrphanConfig{
		Enabled: true,
		Grace:   7 * 24 * time.Hour,
	})
	path := writeMediaFile(t, mediaStore.Root(), "188765450002/20200101/7.jpg", testNow().Add(-time.Hour))

	summary := scanner.ScanOnce(context.Background())
	assert.Zero(t, summary.Deleted)
	_, err := os.Stat(path)
	assert.NoError(t, err, "new file in an old directory must be kept")
}

// 超过宽限期且库中无记录的文件被删除。
func TestOrphanScanDeletesExpiredOrphan(t *testing.T) {
	store := &fakeRetentionStore{}
	scanner, mediaStore, metrics := newOrphanTestScanner(t, store, OrphanConfig{
		Enabled: true,
		Grace:   7 * 24 * time.Hour,
	})
	path := writeMediaFile(t, mediaStore.Root(), "188765450003/20231001/3.jpg", testNow().Add(-30*24*time.Hour))

	summary := scanner.ScanOnce(context.Background())
	assert.EqualValues(t, 1, summary.Deleted)
	assert.Zero(t, summary.Failures)
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "expired orphan should be removed")

	snapshot := metrics.OrphanSnapshot()
	assert.EqualValues(t, 1, snapshot.Deleted)
	assert.EqualValues(t, summary.Scanned, snapshot.Scanned)
}

// 库中仍有对应相对路径的文件不删除。
func TestOrphanScanKeepsReferencedFile(t *testing.T) {
	const relative = "188765450004/20231001/4.jpg"
	store := &fakeRetentionStore{existing: map[string]struct{}{relative: {}}}
	scanner, mediaStore, _ := newOrphanTestScanner(t, store, OrphanConfig{
		Enabled: true,
		Grace:   7 * 24 * time.Hour,
	})
	path := writeMediaFile(t, mediaStore.Root(), relative, testNow().Add(-30*24*time.Hour))

	summary := scanner.ScanOnce(context.Background())
	assert.Zero(t, summary.Deleted)
	assert.GreaterOrEqual(t, summary.Skipped, int64(1))
	_, err := os.Stat(path)
	assert.NoError(t, err, "referenced file must be kept")
}

// 关闭状态不扫描、不删除任何文件。
func TestOrphanScanDisabledLeavesFilesUntouched(t *testing.T) {
	scanner, mediaStore, metrics := newOrphanTestScanner(t, &fakeRetentionStore{}, OrphanConfig{
		Enabled: false,
		Grace:   7 * 24 * time.Hour,
	})
	path := writeMediaFile(t, mediaStore.Root(), "188765450005/20231001/5.jpg", testNow().Add(-30*24*time.Hour))

	summary := scanner.ScanOnce(context.Background())
	assert.Equal(t, OrphanSummary{}, summary)
	_, err := os.Stat(path)
	assert.NoError(t, err, "disabled scan must not touch files")
	snapshot := metrics.OrphanSnapshot()
	assert.Zero(t, snapshot.Scanned)
	assert.Zero(t, snapshot.Deleted)
}

// 单次扫描达到 maxFilesPerRun 后停止，避免一次遍历打满 IO。
func TestOrphanScanStopsAtLimit(t *testing.T) {
	scanner, mediaStore, _ := newOrphanTestScanner(t, &fakeRetentionStore{}, OrphanConfig{
		Enabled:        true,
		Grace:          7 * 24 * time.Hour,
		MaxFilesPerRun: 2,
		BatchSize:      1,
	})
	old := testNow().Add(-30 * 24 * time.Hour)
	for _, name := range []string{"1.jpg", "2.jpg", "3.jpg"} {
		writeMediaFile(t, mediaStore.Root(), "188765450006/20231001/"+name, old)
	}

	summary := scanner.ScanOnce(context.Background())
	assert.True(t, summary.Limited)
	assert.EqualValues(t, 2, summary.Deleted)
	assert.EqualValues(t, 2, summary.Scanned)
}

// 批量扫描按批次比对数据库记录，超过一批时仍只删除孤儿。
func TestOrphanScanDeletesInBatches(t *testing.T) {
	old := testNow().Add(-30 * 24 * time.Hour)
	referenced := "188765450007/20231001/2.jpg"
	store := &fakeRetentionStore{existing: map[string]struct{}{referenced: {}}}
	scanner, mediaStore, _ := newOrphanTestScanner(t, store, OrphanConfig{
		Enabled:   true,
		Grace:     7 * 24 * time.Hour,
		BatchSize: 1,
	})
	for _, name := range []string{"1.jpg", "2.jpg", "3.jpg"} {
		writeMediaFile(t, mediaStore.Root(), "188765450007/20231001/"+name, old)
	}

	summary := scanner.ScanOnce(context.Background())
	assert.EqualValues(t, 2, summary.Deleted)
	assert.GreaterOrEqual(t, summary.Skipped, int64(1))
	_, err := os.Stat(filepath.Join(mediaStore.Root(), filepath.FromSlash(referenced)))
	assert.NoError(t, err, "referenced file must survive batched scan")
}
