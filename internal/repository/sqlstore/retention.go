package sqlstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// retentionStore 是 repository.RetentionStore 的 SQL 实现。
// 所有删除都按主键分批执行，单条 SQL 最多影响 limit 行，避免长时间持锁。
type retentionStore struct {
	store *Store
}

// RetentionStore 返回历史数据清理仓储。
func (s *Store) RetentionStore() repository.RetentionStore {
	return &retentionStore{store: s}
}

type locationKey struct {
	phone    string
	recordID int64
}

type alarmKey struct {
	phone         string
	messageSerial int64
	receivedAt    int64
}

type captureKey struct {
	phone    string
	recordID string
}

type mediaSearchKey struct {
	phone        string
	answerSerial int64
	itemIndex    int64
}

// PurgeLocations 删除至多 limit 条接收时间早于 cutoff 的定位记录。
func (r *retentionStore) PurgeLocations(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("purge locations: limit must be positive, got %d", limit)
	}
	rows, err := r.store.db.QueryContext(ctx,
		`SELECT phone, record_id FROM locations
		 WHERE received_at_ms < ? ORDER BY received_at_ms ASC LIMIT ?`,
		cutoff.UnixMilli(), limit)
	if err != nil {
		return 0, fmt.Errorf("select expired locations: %w", err)
	}
	defer rows.Close()

	keys := make([]locationKey, 0, limit)
	for rows.Next() {
		var key locationKey
		if err := rows.Scan(&key.phone, &key.recordID); err != nil {
			return 0, fmt.Errorf("scan expired location key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate expired locations: %w", err)
	}
	// SQLite 连接池上限为 1，必须先释放查询连接再执行 DELETE。
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close expired locations cursor: %w", err)
	}
	if len(keys) == 0 {
		return 0, nil
	}

	conds := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys)*2)
	for _, key := range keys {
		conds = append(conds, "(phone = ? AND record_id = ?)")
		args = append(args, key.phone, key.recordID)
	}
	result, err := r.store.db.ExecContext(ctx,
		`DELETE FROM locations WHERE `+strings.Join(conds, " OR "), args...)
	if err != nil {
		return 0, fmt.Errorf("delete expired locations: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted locations: %w", err)
	}
	return deleted, nil
}

// PurgeAlarms 删除至多 limit 条接收时间早于 cutoff 的报警记录。
func (r *retentionStore) PurgeAlarms(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("purge alarms: limit must be positive, got %d", limit)
	}
	rows, err := r.store.db.QueryContext(ctx,
		`SELECT phone, message_serial, received_at_ms FROM alarms
		 WHERE received_at_ms < ? ORDER BY received_at_ms ASC LIMIT ?`,
		cutoff.UnixMilli(), limit)
	if err != nil {
		return 0, fmt.Errorf("select expired alarms: %w", err)
	}
	defer rows.Close()

	keys := make([]alarmKey, 0, limit)
	for rows.Next() {
		var key alarmKey
		if err := rows.Scan(&key.phone, &key.messageSerial, &key.receivedAt); err != nil {
			return 0, fmt.Errorf("scan expired alarm key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate expired alarms: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close expired alarms cursor: %w", err)
	}
	if len(keys) == 0 {
		return 0, nil
	}

	conds := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys)*3)
	for _, key := range keys {
		conds = append(conds, "(phone = ? AND message_serial = ? AND received_at_ms = ?)")
		args = append(args, key.phone, key.messageSerial, key.receivedAt)
	}
	result, err := r.store.db.ExecContext(ctx,
		`DELETE FROM alarms WHERE `+strings.Join(conds, " OR "), args...)
	if err != nil {
		return 0, fmt.Errorf("delete expired alarms: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted alarms: %w", err)
	}
	return deleted, nil
}

// ListExpiredMediaFiles 返回至多 limit 条接收时间早于 cutoff 的媒体文件元数据。
// 该方法是只读的，调用方删除文件后再调用 DeleteMediaFiles 删除对应记录。
func (r *retentionStore) ListExpiredMediaFiles(ctx context.Context, cutoff time.Time, limit int) ([]repository.MediaFile, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("list expired media files: limit must be positive, got %d", limit)
	}
	rows, err := r.store.db.QueryContext(ctx,
		`SELECT `+mediaFileColumns+` FROM media_files
		 WHERE received_at < ? ORDER BY received_at ASC, media_id ASC LIMIT ?`,
		cutoff.UnixMilli(), limit)
	if err != nil {
		return nil, fmt.Errorf("select expired media files: %w", err)
	}
	defer rows.Close()

	files := make([]repository.MediaFile, 0, limit)
	for rows.Next() {
		file, err := scanMediaFile(rows)
		if err != nil {
			return nil, fmt.Errorf("scan expired media file: %w", err)
		}
		files = append(files, *file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired media files: %w", err)
	}
	return files, nil
}

// DeleteMediaFiles 按 (phone, media_id) 删除调用方已确认文件不存在的元数据。
func (r *retentionStore) DeleteMediaFiles(ctx context.Context, files []repository.MediaFile) (int64, error) {
	if len(files) == 0 {
		return 0, nil
	}
	conds := make([]string, 0, len(files))
	args := make([]any, 0, len(files)*2)
	for _, file := range files {
		conds = append(conds, "(phone = ? AND media_id = ?)")
		args = append(args, file.Phone, int64(file.MediaID))
	}
	result, err := r.store.db.ExecContext(ctx,
		`DELETE FROM media_files WHERE `+strings.Join(conds, " OR "), args...)
	if err != nil {
		return 0, fmt.Errorf("delete media files: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted media files: %w", err)
	}
	return deleted, nil
}

// PurgeMediaCaptures 删除至多 limit 条接收时间早于 cutoff 的拍摄应答。
// media_capture_items 与主表在同一事务内删除，避免留下孤立明细。
func (r *retentionStore) PurgeMediaCaptures(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("purge media captures: limit must be positive, got %d", limit)
	}
	rows, err := r.store.db.QueryContext(ctx,
		`SELECT phone, record_id FROM media_captures
		 WHERE received_at < ? ORDER BY received_at ASC LIMIT ?`,
		cutoff.UnixMilli(), limit)
	if err != nil {
		return 0, fmt.Errorf("select expired media captures: %w", err)
	}
	defer rows.Close()

	keys := make([]captureKey, 0, limit)
	for rows.Next() {
		var key captureKey
		if err := rows.Scan(&key.phone, &key.recordID); err != nil {
			return 0, fmt.Errorf("scan expired media capture key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate expired media captures: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close expired media captures cursor: %w", err)
	}
	if len(keys) == 0 {
		return 0, nil
	}

	conds := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys)*2)
	for _, key := range keys {
		conds = append(conds, "(phone = ? AND record_id = ?)")
		args = append(args, key.phone, key.recordID)
	}
	where := strings.Join(conds, " OR ")

	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin media capture purge: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM media_capture_items WHERE `+where, args...); err != nil {
		return 0, fmt.Errorf("delete media capture items: %w", err)
	}
	result, err := tx.ExecContext(ctx,
		`DELETE FROM media_captures WHERE `+where, args...)
	if err != nil {
		return 0, fmt.Errorf("delete media captures: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted media captures: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit media capture purge: %w", err)
	}
	return deleted, nil
}

var _ repository.RetentionStore = (*retentionStore)(nil)

// PurgeMediaSearchResults 删除至多 limit 条接收时间早于 cutoff 的检索应答记录。
// 主键为 (phone, answer_serial, item_index)，按主键定位删除。
func (r *retentionStore) PurgeMediaSearchResults(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("purge media search results: limit must be positive, got %d", limit)
	}
	rows, err := r.store.db.QueryContext(ctx,
		`SELECT phone, answer_serial, item_index FROM media_search_results
		 WHERE received_at < ? ORDER BY received_at ASC LIMIT ?`,
		cutoff.UnixMilli(), limit)
	if err != nil {
		return 0, fmt.Errorf("select expired media search results: %w", err)
	}
	defer rows.Close()

	keys := make([]mediaSearchKey, 0, limit)
	for rows.Next() {
		var key mediaSearchKey
		if err := rows.Scan(&key.phone, &key.answerSerial, &key.itemIndex); err != nil {
			return 0, fmt.Errorf("scan expired media search result key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate expired media search results: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close expired media search results cursor: %w", err)
	}
	if len(keys) == 0 {
		return 0, nil
	}

	conds := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys)*3)
	for _, key := range keys {
		conds = append(conds, "(phone = ? AND answer_serial = ? AND item_index = ?)")
		args = append(args, key.phone, key.answerSerial, key.itemIndex)
	}
	result, err := r.store.db.ExecContext(ctx,
		`DELETE FROM media_search_results WHERE `+strings.Join(conds, " OR "), args...)
	if err != nil {
		return 0, fmt.Errorf("delete expired media search results: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted media search results: %w", err)
	}
	return deleted, nil
}

// existingMediaPathChunk 是单条 IN 查询的路径数上限，避免超过 SQLite 的参数上限。
const existingMediaPathChunk = 400

// ExistingMediaPaths 返回入参中仍存在于 media_files 的相对路径。
// 逐个前缀拼接会超过数据库参数上限，因此按固定分片查询。
func (r *retentionStore) ExistingMediaPaths(ctx context.Context, relativePaths []string) (map[string]struct{}, error) {
	existing := make(map[string]struct{}, len(relativePaths))
	if len(relativePaths) == 0 {
		return existing, nil
	}
	for start := 0; start < len(relativePaths); start += existingMediaPathChunk {
		end := start + existingMediaPathChunk
		if end > len(relativePaths) {
			end = len(relativePaths)
		}
		chunk := relativePaths[start:end]
		placeholders := make([]string, 0, len(chunk))
		args := make([]any, 0, len(chunk))
		for _, path := range chunk {
			placeholders = append(placeholders, "?")
			args = append(args, path)
		}
		rows, err := r.store.db.QueryContext(ctx,
			`SELECT relative_path FROM media_files WHERE relative_path IN (`+strings.Join(placeholders, ", ")+`)`,
			args...)
		if err != nil {
			return nil, fmt.Errorf("select existing media paths: %w", err)
		}
		for rows.Next() {
			var path string
			if err := rows.Scan(&path); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan existing media path: %w", err)
			}
			existing[path] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("iterate existing media paths: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("close existing media paths cursor: %w", err)
		}
	}
	return existing, nil
}
