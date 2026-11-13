package sqlcore

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

const attachmentColumns = `source, phone, alarm_no, file_name, terminal_id, alarm_sign, alarm_serial,
	file_type, total_size, received_bytes, watermark, staging_path, staging_base,
	ranges, segments, status, created_at, updated_at, completed_at`

type attachmentFileRepository struct {
	store *Store
}

// AttachmentFileRepository 返回附件元数据仓储。
func (s *Store) AttachmentFileRepository() repository.AttachmentFileRepository {
	return &attachmentFileRepository{store: s}
}

func (r *attachmentFileRepository) Save(ctx context.Context, file *repository.AttachmentFile) (*repository.AttachmentFile, error) {
	if file == nil || file.Phone == "" || file.FileName == "" {
		return nil, fmt.Errorf("save attachment file: nil file, empty phone or file name")
	}
	if file.Source == "" {
		file.Source = repository.AttachmentSourceSubia
	}
	if file.Status == "" {
		file.Status = repository.AttachmentStatusReceiving
	}
	now := time.Now()
	if file.CreatedAt.IsZero() {
		file.CreatedAt = now
	}
	file.UpdatedAt = now
	if file.Status == repository.AttachmentStatusCompleted && file.CompletedAt.IsZero() {
		file.CompletedAt = now
	}
	stmt := r.store.stmts.UpsertAttachmentFile()
	_, err := r.store.db.ExecContext(ctx, stmt,
		file.Source, file.Phone, file.AlarmNo, file.FileName, file.TerminalID,
		file.AlarmSign, file.AlarmSerial, int16(file.FileType), file.TotalSize,
		file.ReceivedBytes, file.Watermark, file.StagingPath, file.StagingBase,
		repository.MarshalRanges(file.Ranges), repository.MarshalSegments(file.Segments),
		file.Status, file.CreatedAt.UnixMilli(), file.UpdatedAt.UnixMilli(), attachmentMillis(file.CompletedAt))
	if err != nil {
		return nil, fmt.Errorf("save attachment file %s/%s: %w", file.Phone, file.FileName, err)
	}
	return file, nil
}

func (r *attachmentFileRepository) Get(ctx context.Context, key repository.AttachmentFileKey) (*repository.AttachmentFile, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	row := r.store.readDB.QueryRowContext(ctx,
		`SELECT `+attachmentColumns+` FROM attachment_files
		 WHERE source = ? AND phone = ? AND alarm_no = ? AND file_name = ?`,
		key.Source, key.Phone, key.AlarmNo, key.FileName)
	file, err := scanAttachmentFile(row)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, repository.ErrAttachmentFileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get attachment file %s/%s: %w", key.Phone, key.FileName, err)
	}
	return file, nil
}

func (r *attachmentFileRepository) FindByFileName(ctx context.Context, source, phone, fileName string) (*repository.AttachmentFile, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	row := r.store.readDB.QueryRowContext(ctx,
		`SELECT `+attachmentColumns+` FROM attachment_files
		 WHERE source = ? AND phone = ? AND file_name = ?
		 ORDER BY updated_at DESC LIMIT 1`, source, phone, fileName)
	file, err := scanAttachmentFile(row)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, repository.ErrAttachmentFileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find attachment file %s/%s: %w", phone, fileName, err)
	}
	return file, nil
}

func (r *attachmentFileRepository) ListByPhone(ctx context.Context, source, phone string, limit int) ([]*repository.AttachmentFile, error) {
	return r.list(ctx, `SELECT `+attachmentColumns+` FROM attachment_files
		WHERE source = ? AND phone = ? ORDER BY updated_at DESC LIMIT ?`, limit, source, phone)
}

func (r *attachmentFileRepository) ListByAlarm(ctx context.Context, source, phone, alarmNo string) ([]*repository.AttachmentFile, error) {
	return r.list(ctx, `SELECT `+attachmentColumns+` FROM attachment_files
		WHERE source = ? AND phone = ? AND alarm_no = ? ORDER BY file_name LIMIT ?`,
		1000, source, phone, alarmNo)
}

func (r *attachmentFileRepository) ListCompletedSource(ctx context.Context, source string, limit int) ([]*repository.AttachmentFile, error) {
	return r.list(ctx, `SELECT `+attachmentColumns+` FROM attachment_files
		WHERE source = ? AND status = ? ORDER BY completed_at DESC LIMIT ?`,
		limit, source, repository.AttachmentStatusCompleted)
}

func (r *attachmentFileRepository) ListReceivingUpdatedBefore(ctx context.Context, cutoff time.Time, limit int) ([]*repository.AttachmentFile, error) {
	return r.list(ctx, `SELECT `+attachmentColumns+` FROM attachment_files
		WHERE status = ? AND updated_at < ? ORDER BY updated_at LIMIT ?`,
		limit, repository.AttachmentStatusReceiving, cutoff.UnixMilli())
}

func (r *attachmentFileRepository) ListCompletedBefore(ctx context.Context, source string, cutoff time.Time, limit int) ([]*repository.AttachmentFile, error) {
	return r.list(ctx, `SELECT `+attachmentColumns+` FROM attachment_files
		WHERE source = ? AND status = ? AND completed_at > 0 AND completed_at < ?
		ORDER BY completed_at LIMIT ?`, limit, source, repository.AttachmentStatusCompleted, cutoff.UnixMilli())
}

func (r *attachmentFileRepository) ListTerminalBefore(ctx context.Context, source string, cutoff time.Time, limit int) ([]*repository.AttachmentFile, error) {
	return r.list(ctx, `SELECT `+attachmentColumns+` FROM attachment_files
		WHERE source = ? AND status IN (?, ?) AND updated_at < ?
		ORDER BY updated_at LIMIT ?`, limit, source,
		repository.AttachmentStatusFailed, repository.AttachmentStatusExpired, cutoff.UnixMilli())
}

func (r *attachmentFileRepository) ReferencedSegment(ctx context.Context, source, key string) (bool, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	var count int64
	err := r.store.readDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM attachment_files WHERE source = ? AND segments LIKE ?`,
		source, "%"+key+"%").Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check attachment segment reference: %w", err)
	}
	return count > 0, nil
}

func (r *attachmentFileRepository) Delete(ctx context.Context, key repository.AttachmentFileKey) error {
	_, err := r.store.db.ExecContext(ctx,
		`DELETE FROM attachment_files WHERE source = ? AND phone = ? AND alarm_no = ? AND file_name = ?`,
		key.Source, key.Phone, key.AlarmNo, key.FileName)
	if err != nil {
		return fmt.Errorf("delete attachment file %s/%s: %w", key.Phone, key.FileName, err)
	}
	return nil
}

func (r *attachmentFileRepository) Stats(ctx context.Context, source string) (int64, int64, int64, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	var files, completed, bytes int64
	err := r.store.readDB.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status = ? THEN total_size ELSE 0 END), 0)
		 FROM attachment_files WHERE source = ?`,
		repository.AttachmentStatusCompleted, repository.AttachmentStatusCompleted, source).
		Scan(&files, &completed, &bytes)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("attachment stats: %w", err)
	}
	return files, completed, bytes, nil
}

func (r *attachmentFileRepository) list(ctx context.Context, query string, limit int, args ...any) ([]*repository.AttachmentFile, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	if limit <= 0 {
		limit = 1000
	}
	args = append(args, limit)
	rows, err := r.store.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list attachment files: %w", err)
	}
	defer rows.Close()
	var out []*repository.AttachmentFile
	for rows.Next() {
		file, err := scanAttachmentFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, file)
	}
	return out, rows.Err()
}

func scanAttachmentFile(row interface{ Scan(...any) error }) (*repository.AttachmentFile, error) {
	var file repository.AttachmentFile
	var fileType int16
	var ranges, segments string
	var createdAt, updatedAt, completedAt int64
	if err := row.Scan(
		&file.Source, &file.Phone, &file.AlarmNo, &file.FileName, &file.TerminalID,
		&file.AlarmSign, &file.AlarmSerial, &fileType, &file.TotalSize,
		&file.ReceivedBytes, &file.Watermark, &file.StagingPath, &file.StagingBase,
		&ranges, &segments, &file.Status, &createdAt, &updatedAt, &completedAt,
	); err != nil {
		return nil, err
	}
	file.FileType = uint8(fileType)
	file.Ranges = []byte(ranges)
	file.Segments = repository.UnmarshalSegments(segments)
	file.CreatedAt = time.UnixMilli(createdAt)
	file.UpdatedAt = time.UnixMilli(updatedAt)
	if completedAt > 0 {
		file.CompletedAt = time.UnixMilli(completedAt)
	}
	return &file, nil
}

func attachmentMillis(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixMilli()
}

var _ repository.AttachmentFileRepository = (*attachmentFileRepository)(nil)
