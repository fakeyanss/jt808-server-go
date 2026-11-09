package sqlstore

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

const (
	mediaFileUpsertSQLite = `INSERT INTO media_files (
		phone, media_id, media_type, media_format, event_id, channel_id,
		alarm_sign, status_sign, latitude, longitude, altitude, speed, direction,
		location_time, relative_path, file_size, md5, received_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(phone, media_id) DO UPDATE SET
		media_type = excluded.media_type, media_format = excluded.media_format,
		event_id = excluded.event_id, channel_id = excluded.channel_id,
		alarm_sign = excluded.alarm_sign, status_sign = excluded.status_sign,
		latitude = excluded.latitude, longitude = excluded.longitude,
		altitude = excluded.altitude, speed = excluded.speed,
		direction = excluded.direction, location_time = excluded.location_time,
		relative_path = excluded.relative_path, file_size = excluded.file_size,
		md5 = excluded.md5, received_at = excluded.received_at`

	mediaFileUpsertMySQL = `INSERT INTO media_files (
		phone, media_id, media_type, media_format, event_id, channel_id,
		alarm_sign, status_sign, latitude, longitude, altitude, speed, direction,
		location_time, relative_path, file_size, md5, received_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		media_type = VALUES(media_type), media_format = VALUES(media_format),
		event_id = VALUES(event_id), channel_id = VALUES(channel_id),
		alarm_sign = VALUES(alarm_sign), status_sign = VALUES(status_sign),
		latitude = VALUES(latitude), longitude = VALUES(longitude),
		altitude = VALUES(altitude), speed = VALUES(speed),
		direction = VALUES(direction), location_time = VALUES(location_time),
		relative_path = VALUES(relative_path), file_size = VALUES(file_size),
		md5 = VALUES(md5), received_at = VALUES(received_at)`

	mediaCaptureUpsertSQLite = `INSERT INTO media_captures (phone, record_id, answer_serial, result, received_at)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(phone, record_id) DO UPDATE SET
		answer_serial = excluded.answer_serial, result = excluded.result,
		received_at = excluded.received_at`

	mediaCaptureUpsertMySQL = `INSERT INTO media_captures (phone, record_id, answer_serial, result, received_at)
	VALUES (?, ?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		answer_serial = VALUES(answer_serial), result = VALUES(result),
		received_at = VALUES(received_at)`
)

const mediaFileColumns = `phone, media_id, media_type, media_format, event_id, channel_id,
	alarm_sign, status_sign, latitude, longitude, altitude, speed, direction,
	location_time, relative_path, file_size, md5, received_at`

// mediaFileRepository 是 repository.MediaFileRepository 与
// repository.MediaCaptureRepository 的 SQL 实现。
type mediaFileRepository struct {
	store *Store
}

// MediaFileRepository 返回媒体文件与拍摄应答仓储。
func (s *Store) MediaFileRepository() repository.MediaFileRepository {
	return &mediaFileRepository{store: s}
}

// MediaCaptureRepository 返回与 MediaFileRepository 共用同一实现的拍摄应答仓储。
func (s *Store) MediaCaptureRepository() repository.MediaCaptureRepository {
	return &mediaFileRepository{store: s}
}

func (r *mediaFileRepository) Save(ctx context.Context, file *repository.MediaFile) error {
	if file == nil || file.Phone == "" || file.MediaID == 0 {
		return fmt.Errorf("save media file: nil file, empty phone or zero media id")
	}
	stmt := mediaFileUpsertSQLite
	if r.store.backend == BackendMySQL {
		stmt = mediaFileUpsertMySQL
	}
	if _, err := r.store.db.ExecContext(ctx, stmt,
		file.Phone, int64(file.MediaID), int16(file.MediaType), int16(file.MediaFormat),
		int16(file.EventID), int16(file.ChannelID), int64(file.AlarmSign), int64(file.StatusSign),
		int64(file.Latitude), int64(file.Longitude), int64(file.Altitude), int64(file.Speed),
		int64(file.Direction), mediaLocationMillis(file.LocationTime), file.RelativePath,
		file.FileSize, file.MD5, file.ReceivedAt.UnixMilli()); err != nil {
		return fmt.Errorf("save media file %s/%d: %w", file.Phone, file.MediaID, err)
	}
	return nil
}

func (r *mediaFileRepository) Get(ctx context.Context, phone string, mediaID uint32) (*repository.MediaFile, error) {
	row := r.store.db.QueryRowContext(ctx,
		`SELECT `+mediaFileColumns+` FROM media_files WHERE phone = ? AND media_id = ?`,
		phone, int64(mediaID))
	file, err := scanMediaFile(row)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, repository.ErrMediaFileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get media file %s/%d: %w", phone, mediaID, err)
	}
	return file, nil
}

func (r *mediaFileRepository) List(ctx context.Context, phone string, filter repository.MediaFileFilter) ([]*repository.MediaFile, error) {
	query := `SELECT ` + mediaFileColumns + ` FROM media_files WHERE phone = ?`
	args := []any{phone}
	if filter.ChannelID != nil {
		query += " AND channel_id = ?"
		args = append(args, int16(*filter.ChannelID))
	}
	if filter.MediaType != nil {
		query += " AND media_type = ?"
		args = append(args, int16(*filter.MediaType))
	}
	if filter.AlarmOnly {
		query += " AND alarm_sign <> 0"
	}
	if filter.Start != nil {
		query += " AND location_time >= ?"
		args = append(args, filter.Start.UnixMilli())
	}
	if filter.End != nil {
		query += " AND location_time <= ?"
		args = append(args, filter.End.UnixMilli())
	}
	query += " ORDER BY received_at DESC, media_id DESC LIMIT ?"
	args = append(args, normalizeMediaLimit(filter.Limit))

	rows, err := r.store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list media files %s: %w", phone, err)
	}
	defer rows.Close()

	files := make([]*repository.MediaFile, 0)
	for rows.Next() {
		file, scanErr := scanMediaFile(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan media file %s: %w", phone, scanErr)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate media files %s: %w", phone, err)
	}
	return files, nil
}

func (r *mediaFileRepository) Stats(ctx context.Context) (int64, int64, error) {
	var count, bytes int64
	if err := r.store.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(file_size), 0) FROM media_files`).Scan(&count, &bytes); err != nil {
		return 0, 0, fmt.Errorf("collect media file stats: %w", err)
	}
	return count, bytes, nil
}

func (r *mediaFileRepository) SaveCapture(ctx context.Context, capture *repository.MediaCapture) error {
	if capture == nil || capture.Phone == "" || capture.RecordID == "" {
		return fmt.Errorf("save media capture: nil capture, empty phone or record id")
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin media capture transaction %s/%s: %w", capture.Phone, capture.RecordID, err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt := mediaCaptureUpsertSQLite
	if r.store.backend == BackendMySQL {
		stmt = mediaCaptureUpsertMySQL
	}
	if _, err := tx.ExecContext(ctx, stmt,
		capture.Phone, capture.RecordID, int64(capture.AnswerSerial), int16(capture.Result),
		capture.ReceivedAt.UnixMilli()); err != nil {
		return fmt.Errorf("save media capture %s/%s: %w", capture.Phone, capture.RecordID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM media_capture_items WHERE phone = ? AND record_id = ?`,
		capture.Phone, capture.RecordID); err != nil {
		return fmt.Errorf("reset media capture items %s/%s: %w", capture.Phone, capture.RecordID, err)
	}
	for index, item := range capture.Items {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO media_capture_items (phone, record_id, item_index, media_id) VALUES (?, ?, ?, ?)`,
			capture.Phone, capture.RecordID, index, int64(item.MediaID)); err != nil {
			return fmt.Errorf("save media capture item %s/%s/%d: %w", capture.Phone, capture.RecordID, index, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit media capture %s/%s: %w", capture.Phone, capture.RecordID, err)
	}
	return nil
}

func (r *mediaFileRepository) ListCaptures(ctx context.Context, phone string, limit int) ([]*repository.MediaCapture, error) {
	rows, err := r.store.db.QueryContext(ctx,
		`SELECT phone, record_id, answer_serial, result, received_at FROM media_captures
		WHERE phone = ? ORDER BY received_at DESC, record_id DESC LIMIT ?`,
		phone, normalizeMediaLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list media captures %s: %w", phone, err)
	}
	defer rows.Close()

	captures := make([]*repository.MediaCapture, 0)
	for rows.Next() {
		capture := &repository.MediaCapture{}
		var (
			answerSerial int64
			result       int16
			receivedAt   int64
		)
		if err := rows.Scan(&capture.Phone, &capture.RecordID, &answerSerial, &result, &receivedAt); err != nil {
			return nil, fmt.Errorf("scan media capture %s: %w", phone, err)
		}
		capture.AnswerSerial = uint16(answerSerial)
		capture.Result = uint8(result)
		capture.ReceivedAt = time.UnixMilli(receivedAt)
		captures = append(captures, capture)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate media captures %s: %w", phone, err)
	}

	for _, capture := range captures {
		items, err := r.listCaptureItems(ctx, capture)
		if err != nil {
			return nil, err
		}
		capture.Items = items
	}
	return captures, nil
}

// listCaptureItems 查询单条拍摄应答的媒体 ID，并关联 media_files 判断文件是否已到达。
func (r *mediaFileRepository) listCaptureItems(ctx context.Context, capture *repository.MediaCapture) ([]repository.MediaCaptureItem, error) {
	rows, err := r.store.db.QueryContext(ctx,
		`SELECT i.media_id, COALESCE(f.file_size, -1) FROM media_capture_items i
		LEFT JOIN media_files f ON f.phone = i.phone AND f.media_id = i.media_id
		WHERE i.phone = ? AND i.record_id = ? ORDER BY i.item_index ASC`,
		capture.Phone, capture.RecordID)
	if err != nil {
		return nil, fmt.Errorf("list media capture items %s/%s: %w", capture.Phone, capture.RecordID, err)
	}
	defer rows.Close()

	items := make([]repository.MediaCaptureItem, 0)
	for rows.Next() {
		var (
			mediaID  int64
			fileSize int64
		)
		if err := rows.Scan(&mediaID, &fileSize); err != nil {
			return nil, fmt.Errorf("scan media capture item %s/%s: %w", capture.Phone, capture.RecordID, err)
		}
		item := repository.MediaCaptureItem{MediaID: uint32(mediaID)}
		if fileSize >= 0 {
			item.Arrived = true
			item.FileSize = fileSize
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate media capture items %s/%s: %w", capture.Phone, capture.RecordID, err)
	}
	return items, nil
}

// scanMediaFile 从一行查询结果读取媒体文件元数据。
func scanMediaFile(row interface{ Scan(...any) error }) (*repository.MediaFile, error) {
	var (
		file                                       repository.MediaFile
		mediaID, receivedAt, locationTime          int64
		mediaType, mediaFormat, eventID, channelID int16
		alarmSign, statusSign, latitude, longitude int64
		altitude, speed, direction                 int64
	)
	if err := row.Scan(
		&file.Phone, &mediaID, &mediaType, &mediaFormat, &eventID, &channelID,
		&alarmSign, &statusSign, &latitude, &longitude, &altitude, &speed, &direction,
		&locationTime, &file.RelativePath, &file.FileSize, &file.MD5, &receivedAt,
	); err != nil {
		return nil, err
	}
	file.MediaID = uint32(mediaID)
	file.MediaType = uint8(mediaType)
	file.MediaFormat = uint8(mediaFormat)
	file.EventID = uint8(eventID)
	file.ChannelID = uint8(channelID)
	file.AlarmSign = uint32(alarmSign)
	file.StatusSign = uint32(statusSign)
	file.Latitude = uint32(latitude)
	file.Longitude = uint32(longitude)
	file.Altitude = uint16(altitude)
	file.Speed = uint16(speed)
	file.Direction = uint16(direction)
	if locationTime != 0 {
		file.LocationTime = time.UnixMilli(locationTime)
	}
	file.ReceivedAt = time.UnixMilli(receivedAt)
	return &file, nil
}

// 保证 mediaFileRepository 同时实现两个仓储接口。
var (
	_ repository.MediaFileRepository    = (*mediaFileRepository)(nil)
	_ repository.MediaCaptureRepository = (*mediaFileRepository)(nil)
)

// normalizeMediaLimit 与 repository 包保持一致，避免 API 层绕过仓储上限。
func normalizeMediaLimit(limit int) int {
	const (
		defaultLimit = 100
		maxLimit     = 1000
	)
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

// mediaLocationMillis 把位置时间转换为毫秒时间戳；零值统一存 0，避免负时间戳污染范围查询。
func mediaLocationMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
