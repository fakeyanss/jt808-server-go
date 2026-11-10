package sqlstore

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// insertLocation 直接写库，便于构造精确的接收时间边界。
func insertLocation(t *testing.T, store *Store, phone string, recordID, receivedAt int64) {
	t.Helper()
	_, err := store.db.ExecContext(context.Background(),
		`INSERT INTO locations (phone, record_id, msg_id, report_time_ms, received_at_ms, payload_json)
		 VALUES (?, ?, 0x0200, ?, ?, '{}')`,
		phone, recordID, receivedAt, receivedAt)
	require.NoError(t, err)
}

func insertAlarm(t *testing.T, store *Store, phone string, serial, receivedAt int64) {
	t.Helper()
	_, err := store.db.ExecContext(context.Background(),
		`INSERT INTO alarms (phone, message_serial, received_at_ms) VALUES (?, ?, ?)`,
		phone, serial, receivedAt)
	require.NoError(t, err)
}

func insertMediaFile(t *testing.T, store *Store, phone string, mediaID uint32, relativePath string, receivedAt int64) {
	t.Helper()
	_, err := store.db.ExecContext(context.Background(),
		`INSERT INTO media_files (phone, media_id, relative_path, file_size, received_at)
		 VALUES (?, ?, ?, 10, ?)`,
		phone, int64(mediaID), relativePath, receivedAt)
	require.NoError(t, err)
}

func insertCapture(t *testing.T, store *Store, phone, recordID string, items []uint32, receivedAt int64) {
	t.Helper()
	_, err := store.db.ExecContext(context.Background(),
		`INSERT INTO media_captures (phone, record_id, answer_serial, received_at) VALUES (?, ?, 1, ?)`,
		phone, recordID, receivedAt)
	require.NoError(t, err)
	for index, mediaID := range items {
		_, err := store.db.ExecContext(context.Background(),
			`INSERT INTO media_capture_items (phone, record_id, item_index, media_id) VALUES (?, ?, ?, ?)`,
			phone, recordID, index, int64(mediaID))
		require.NoError(t, err)
	}
}

func countRows(t *testing.T, store *Store, query string, args ...any) int {
	t.Helper()
	var count int
	require.NoError(t, store.db.QueryRowContext(context.Background(), query, args...).Scan(&count))
	return count
}

// 按时间边界删除：严格早于 cutoff 的删除，等于或晚于 cutoff 的保留；分批每次最多 limit 行。
func TestSQLitePurgeLocationsByTimeAndBatch(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)
	retention := store.RetentionStore()
	cutoff := time.UnixMilli(1_700_000_000_000)
	phone := "188765430001"

	insertLocation(t, store, phone, 1, cutoff.UnixMilli()-2000)
	insertLocation(t, store, phone, 2, cutoff.UnixMilli()-1000)
	insertLocation(t, store, phone, 3, cutoff.UnixMilli()) // 等于 cutoff，保留
	insertLocation(t, store, phone, 4, cutoff.UnixMilli()+1000)

	deleted, err := retention.PurgeLocations(ctx, cutoff, 1)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)
	assert.Equal(t, 3, countRows(t, store, `SELECT COUNT(*) FROM locations`))

	deleted, err = retention.PurgeLocations(ctx, cutoff, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)
	assert.Equal(t, 2, countRows(t, store,
		`SELECT COUNT(*) FROM locations WHERE phone = ?`, phone))

	// 保留记录仍是 cutoff 及之后的两条。
	assert.Equal(t, 0, countRows(t, store,
		`SELECT COUNT(*) FROM locations WHERE received_at_ms < ?`, cutoff.UnixMilli()))

	deleted, err = retention.PurgeLocations(ctx, cutoff, 10)
	require.NoError(t, err)
	assert.Zero(t, deleted)
}

// 报警按接收时间清理，边界记录保留。
func TestSQLitePurgeAlarmsByTime(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)
	retention := store.RetentionStore()
	cutoff := time.UnixMilli(1_700_000_000_000)
	phone := "188765430002"

	insertAlarm(t, store, phone, 1, cutoff.UnixMilli()-1)
	insertAlarm(t, store, phone, 2, cutoff.UnixMilli())
	insertAlarm(t, store, phone, 3, cutoff.UnixMilli()+1)

	deleted, err := retention.PurgeAlarms(ctx, cutoff, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)
	assert.Equal(t, 2, countRows(t, store, `SELECT COUNT(*) FROM alarms`))
	assert.Equal(t, 0, countRows(t, store,
		`SELECT COUNT(*) FROM alarms WHERE phone = ? AND message_serial = 1`, phone))
}

// 媒体文件先查询过期元数据，确认文件删除后再按主键删库。
func TestSQLiteExpiredMediaFilesAndDelete(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)
	retention := store.RetentionStore()
	cutoff := time.UnixMilli(1_700_000_000_000)
	phone := "188765430003"

	insertMediaFile(t, store, phone, 100, "188765430003/20231114/100.jpg", cutoff.UnixMilli()-1)
	insertMediaFile(t, store, phone, 101, "188765430003/20231114/101.jpg", cutoff.UnixMilli())

	files, err := retention.ListExpiredMediaFiles(ctx, cutoff, 10)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, uint32(100), files[0].MediaID)
	assert.Equal(t, "188765430003/20231114/100.jpg", files[0].RelativePath)

	deleted, err := retention.DeleteMediaFiles(ctx, files)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)

	files, err = retention.ListExpiredMediaFiles(ctx, cutoff, 10)
	require.NoError(t, err)
	assert.Empty(t, files)

	// 保留的记录仍可通过仓储读到。
	got, err := store.MediaFileRepository().Get(ctx, phone, 101)
	require.NoError(t, err)
	assert.Equal(t, uint32(101), got.MediaID)

	deleted, err = retention.DeleteMediaFiles(ctx, nil)
	require.NoError(t, err)
	assert.Zero(t, deleted)
}

// 拍摄应答清理必须在同一事务内删除明细，避免孤立 media_capture_items。
func TestSQLitePurgeMediaCapturesDeletesItems(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)
	retention := store.RetentionStore()
	cutoff := time.UnixMilli(1_700_000_000_000)
	phone := "188765430004"

	insertCapture(t, store, phone, "old", []uint32{1, 2}, cutoff.UnixMilli()-1)
	insertCapture(t, store, phone, "new", []uint32{3}, cutoff.UnixMilli())

	deleted, err := retention.PurgeMediaCaptures(ctx, cutoff, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM media_captures`))
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM media_capture_items`))
	assert.Equal(t, 0, countRows(t, store,
		`SELECT COUNT(*) FROM media_capture_items WHERE record_id = 'old'`))
	assert.Equal(t, 1, countRows(t, store,
		`SELECT COUNT(*) FROM media_capture_items WHERE record_id = 'new'`))
}

// 非法批大小必须报错，避免调用方误传 0 导致无界删除或空转。
func TestSQLiteRetentionRejectsNonPositiveLimit(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)
	retention := store.RetentionStore()
	cutoff := time.Now()

	_, err := retention.PurgeLocations(ctx, cutoff, 0)
	require.Error(t, err)
	_, err = retention.PurgeAlarms(ctx, cutoff, -1)
	require.Error(t, err)
	_, err = retention.ListExpiredMediaFiles(ctx, cutoff, 0)
	require.Error(t, err)
	_, err = retention.PurgeMediaCaptures(ctx, cutoff, 0)
	require.Error(t, err)
	_, err = retention.PurgeMediaSearchResults(ctx, cutoff, 0)
	require.Error(t, err)
}

func insertMediaSearchResult(t *testing.T, store *Store, phone string, answerSerial, index, receivedAt int64) {
	t.Helper()
	_, err := store.db.ExecContext(context.Background(),
		`INSERT INTO media_search_results (phone, answer_serial, item_index, media_id, received_at)
		 VALUES (?, ?, ?, ?, ?)`,
		phone, answerSerial, index, answerSerial*100+index, receivedAt)
	require.NoError(t, err)
}

// 检索应答按自身 received_at 清理，边界保留，且分批执行。
func TestSQLitePurgeMediaSearchResultsByTimeAndBatch(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)
	retention := store.RetentionStore()
	cutoff := time.UnixMilli(1_700_000_000_000)
	phone := "188765430005"

	insertMediaSearchResult(t, store, phone, 1, 0, cutoff.UnixMilli()-2000)
	insertMediaSearchResult(t, store, phone, 1, 1, cutoff.UnixMilli()-1000)
	insertMediaSearchResult(t, store, phone, 2, 0, cutoff.UnixMilli()) // 等于 cutoff，保留
	insertMediaSearchResult(t, store, phone, 3, 0, cutoff.UnixMilli()+1000)

	deleted, err := retention.PurgeMediaSearchResults(ctx, cutoff, 1)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)
	assert.Equal(t, 3, countRows(t, store, `SELECT COUNT(*) FROM media_search_results`))

	deleted, err = retention.PurgeMediaSearchResults(ctx, cutoff, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)
	assert.Equal(t, 2, countRows(t, store, `SELECT COUNT(*) FROM media_search_results`))
	assert.Equal(t, 0, countRows(t, store,
		`SELECT COUNT(*) FROM media_search_results WHERE received_at < ?`, cutoff.UnixMilli()))

	deleted, err = retention.PurgeMediaSearchResults(ctx, cutoff, 10)
	require.NoError(t, err)
	assert.Zero(t, deleted)
}

// 孤儿扫描按相对路径精确比对，只有库中存在的路径被返回。
func TestSQLiteExistingMediaPaths(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)
	retention := store.RetentionStore()
	phone := "188765430006"

	insertMediaFile(t, store, phone, 200, "188765430006/20231001/200.jpg", 1)
	insertMediaFile(t, store, phone, 201, "188765430006/20231001/201.jpg", 1)

	existing, err := retention.ExistingMediaPaths(ctx, []string{
		"188765430006/20231001/200.jpg",
		"188765430006/20231001/999.jpg",
	})
	require.NoError(t, err)
	assert.Contains(t, existing, "188765430006/20231001/200.jpg")
	assert.NotContains(t, existing, "188765430006/20231001/999.jpg")

	existing, err = retention.ExistingMediaPaths(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, existing)
}

// 确认 SQL 实现满足 repository.RetentionStore 契约。
var _ repository.RetentionStore = (*retentionStore)(nil)
