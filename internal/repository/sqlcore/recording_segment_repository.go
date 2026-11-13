package sqlcore

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

const recordingSegmentColumns = `id, phone, channel, kind, object_key, local_path,
	start_at, end_at, size, keyframes, state, created_at`

// recordingSegmentRepository 是 repository.RecordingSegmentRepository 的 SQL 实现。
type recordingSegmentRepository struct {
	store *Store
}

// RecordingSegmentRepository 返回录像切片索引仓储。
func (s *Store) RecordingSegmentRepository() repository.RecordingSegmentRepository {
	return &recordingSegmentRepository{store: s}
}

func (r *recordingSegmentRepository) Save(ctx context.Context, segment *repository.RecordingSegment) error {
	if segment == nil || segment.ID == "" || segment.Phone == "" {
		return fmt.Errorf("save recording segment: nil segment, empty id or phone")
	}
	createdAt := segment.CreatedAt
	if createdAt.IsZero() {
		createdAt = segment.StartAt
	}
	if segment.Keyframes == nil {
		segment.Keyframes = []byte("[]")
	}
	stmt := r.store.stmts.UpsertRecordingSegment()
	if _, err := r.store.db.ExecContext(ctx, stmt,
		segment.ID, segment.Phone, int16(segment.Channel), segment.Kind,
		segment.ObjectKey, segment.LocalPath, segment.StartAt.UnixMilli(), segment.EndAt.UnixMilli(),
		segment.Size, string(segment.Keyframes), segment.State, createdAt.UnixMilli()); err != nil {
		return fmt.Errorf("save recording segment %s: %w", segment.ID, err)
	}
	return nil
}

func (r *recordingSegmentRepository) Get(ctx context.Context, phone, id string) (*repository.RecordingSegment, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	row := r.store.readDB.QueryRowContext(ctx,
		`SELECT `+recordingSegmentColumns+` FROM recording_segments WHERE id = ? AND phone = ?`, id, phone)
	segment, err := scanRecordingSegment(row)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, repository.ErrRecordingSegmentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get recording segment %s: %w", id, err)
	}
	return segment, nil
}

func (r *recordingSegmentRepository) ListByRange(ctx context.Context, phone string, channel uint8, start, end time.Time) ([]*repository.RecordingSegment, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	rows, err := r.store.readDB.QueryContext(ctx,
		`SELECT `+recordingSegmentColumns+` FROM recording_segments
		 WHERE phone = ? AND channel = ? AND end_at > ? AND start_at < ?
		 ORDER BY start_at ASC, id ASC LIMIT 1000`,
		phone, int16(channel), start.UnixMilli(), end.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("list recording segments %s/%d: %w", phone, channel, err)
	}
	defer rows.Close()
	items := make([]*repository.RecordingSegment, 0)
	for rows.Next() {
		segment, scanErr := scanRecordingSegment(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan recording segment %s/%d: %w", phone, channel, scanErr)
		}
		items = append(items, segment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recording segments %s/%d: %w", phone, channel, err)
	}
	return items, nil
}

func (r *recordingSegmentRepository) ListExpired(ctx context.Context, cutoff time.Time, limit int) ([]*repository.RecordingSegment, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	if limit <= 0 {
		limit = 1000
	}
	rows, err := r.store.readDB.QueryContext(ctx,
		`SELECT `+recordingSegmentColumns+` FROM recording_segments
		 WHERE end_at < ? ORDER BY end_at ASC, id ASC LIMIT ?`, cutoff.UnixMilli(), limit)
	if err != nil {
		return nil, fmt.Errorf("list expired recording segments: %w", err)
	}
	defer rows.Close()
	items := make([]*repository.RecordingSegment, 0)
	for rows.Next() {
		segment, scanErr := scanRecordingSegment(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan expired recording segment: %w", scanErr)
		}
		items = append(items, segment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired recording segments: %w", err)
	}
	return items, nil
}

func (r *recordingSegmentRepository) Delete(ctx context.Context, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	result, err := r.store.db.ExecContext(ctx,
		`DELETE FROM recording_segments WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return 0, fmt.Errorf("delete recording segments: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete recording segments rows affected: %w", err)
	}
	return deleted, nil
}

func (r *recordingSegmentRepository) ReferencedSegment(ctx context.Context, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	ctx, cancel := readContext(ctx)
	defer cancel()
	var count int64
	if err := r.store.readDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM recording_segments WHERE object_key = ?`, key).Scan(&count); err != nil {
		return false, fmt.Errorf("check recording segment reference: %w", err)
	}
	return count > 0, nil
}

func (r *recordingSegmentRepository) Stats(ctx context.Context) (int64, int64, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	var count, bytes int64
	if err := r.store.readDB.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(size), 0) FROM recording_segments`).Scan(&count, &bytes); err != nil {
		return 0, 0, fmt.Errorf("collect recording segment stats: %w", err)
	}
	return count, bytes, nil
}

func scanRecordingSegment(row interface{ Scan(...any) error }) (*repository.RecordingSegment, error) {
	var (
		segment        repository.RecordingSegment
		channel        int16
		startAt, endAt int64
		createdAt      int64
		keyframes      string
	)
	if err := row.Scan(&segment.ID, &segment.Phone, &channel, &segment.Kind,
		&segment.ObjectKey, &segment.LocalPath, &startAt, &endAt, &segment.Size,
		&keyframes, &segment.State, &createdAt); err != nil {
		return nil, err
	}
	segment.Channel = uint8(channel)
	segment.StartAt = time.UnixMilli(startAt)
	segment.EndAt = time.UnixMilli(endAt)
	segment.CreatedAt = time.UnixMilli(createdAt)
	segment.Keyframes = []byte(keyframes)
	return &segment, nil
}

var _ repository.RecordingSegmentRepository = (*recordingSegmentRepository)(nil)
