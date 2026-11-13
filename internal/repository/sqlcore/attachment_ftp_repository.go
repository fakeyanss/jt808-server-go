package sqlcore

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

const attachmentFTPTaskColumns = `phone, serial, username, secret_hash, upload_path, channel,
	start_time, end_time, alarm_sign, media_type, stream_type, storage_type, task_condition,
	status, node_id, control_seq, total_bytes, revoked_at, created_at, updated_at, completed_at`

type attachmentFTPTaskRepository struct {
	store *Store
}

// AttachmentFTPTaskRepository 返回 FTP 上传任务仓储。
func (s *Store) AttachmentFTPTaskRepository() repository.AttachmentFTPTaskRepository {
	return &attachmentFTPTaskRepository{store: s}
}

func (r *attachmentFTPTaskRepository) Save(ctx context.Context, task *repository.AttachmentFTPTask) (*repository.AttachmentFTPTask, error) {
	if task == nil || task.Phone == "" || task.Username == "" {
		return nil, fmt.Errorf("save attachment ftp task: nil task, empty phone or username")
	}
	if task.Status == "" {
		task.Status = repository.FTPTaskStatusIssued
	}
	now := r.store.clock.Now()
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	task.UpdatedAt = now
	if task.Status == repository.FTPTaskStatusCompleted && task.CompletedAt.IsZero() {
		task.CompletedAt = now
	}
	_, err := r.store.db.ExecContext(ctx, r.store.stmts.UpsertAttachmentFTPTask(),
		task.Phone, task.Serial, task.Username, task.SecretHash, task.UploadPath, int16(task.Channel),
		attachmentMillis(task.StartTime), attachmentMillis(task.EndTime), task.AlarmSign,
		int16(task.MediaType), int16(task.StreamType), int16(task.StorageType), int16(task.TaskCondition),
		task.Status, task.NodeID, int64(task.ControlSeq), task.TotalBytes,
		attachmentMillis(task.RevokedAt), task.CreatedAt.UnixMilli(), task.UpdatedAt.UnixMilli(),
		attachmentMillis(task.CompletedAt))
	if err != nil {
		return nil, fmt.Errorf("save attachment ftp task %s/%d: %w", task.Phone, task.Serial, err)
	}
	return task, nil
}

func (r *attachmentFTPTaskRepository) Get(ctx context.Context, key repository.AttachmentFTPTaskKey) (*repository.AttachmentFTPTask, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	row := r.store.readDB.QueryRowContext(ctx,
		`SELECT `+attachmentFTPTaskColumns+` FROM attachment_ftp_tasks WHERE phone = ? AND serial = ?`,
		key.Phone, key.Serial)
	task, err := scanAttachmentFTPTask(row)
	if err == stdsql.ErrNoRows {
		return nil, repository.ErrAttachmentFTPTaskNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get attachment ftp task %s/%d: %w", key.Phone, key.Serial, err)
	}
	return task, nil
}

func (r *attachmentFTPTaskRepository) GetByUsername(ctx context.Context, username string) (*repository.AttachmentFTPTask, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	row := r.store.readDB.QueryRowContext(ctx,
		`SELECT `+attachmentFTPTaskColumns+` FROM attachment_ftp_tasks WHERE username = ? LIMIT 1`, username)
	task, err := scanAttachmentFTPTask(row)
	if err == stdsql.ErrNoRows {
		return nil, repository.ErrAttachmentFTPTaskNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get attachment ftp task by username: %w", err)
	}
	return task, nil
}

func (r *attachmentFTPTaskRepository) ListByStatus(ctx context.Context, status string, updatedBefore time.Time, limit int) ([]*repository.AttachmentFTPTask, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	if limit <= 0 {
		limit = 200
	}
	query := `SELECT ` + attachmentFTPTaskColumns + ` FROM attachment_ftp_tasks WHERE status = ?`
	args := []any{status}
	if !updatedBefore.IsZero() {
		query += ` AND updated_at < ?`
		args = append(args, updatedBefore.UnixMilli())
	}
	query += ` ORDER BY updated_at LIMIT ?`
	args = append(args, limit)
	rows, err := r.store.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list attachment ftp tasks: %w", err)
	}
	defer rows.Close()
	out := make([]*repository.AttachmentFTPTask, 0)
	for rows.Next() {
		task, scanErr := scanAttachmentFTPTask(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

func (r *attachmentFTPTaskRepository) Delete(ctx context.Context, key repository.AttachmentFTPTaskKey) error {
	_, err := r.store.db.ExecContext(ctx,
		`DELETE FROM attachment_ftp_tasks WHERE phone = ? AND serial = ?`, key.Phone, key.Serial)
	if err != nil {
		return fmt.Errorf("delete attachment ftp task %s/%d: %w", key.Phone, key.Serial, err)
	}
	return nil
}

func scanAttachmentFTPTask(row interface{ Scan(...any) error }) (*repository.AttachmentFTPTask, error) {
	var (
		task      repository.AttachmentFTPTask
		channel   int16
		mediaType int16
		stream    int16
		storage   int16
		condition int16
		control   int64
		startTime int64
		endTime   int64
		revokedAt int64
		createdAt int64
		updatedAt int64
		completed int64
	)
	if err := row.Scan(
		&task.Phone, &task.Serial, &task.Username, &task.SecretHash, &task.UploadPath, &channel,
		&startTime, &endTime, &task.AlarmSign, &mediaType, &stream, &storage, &condition,
		&task.Status, &task.NodeID, &control, &task.TotalBytes, &revokedAt,
		&createdAt, &updatedAt, &completed,
	); err != nil {
		return nil, err
	}
	task.Channel = uint8(channel)
	task.MediaType = uint8(mediaType)
	task.StreamType = uint8(stream)
	task.StorageType = uint8(storage)
	task.TaskCondition = uint8(condition)
	task.ControlSeq = uint32(control)
	task.StartTime = timeFromUnixMilli(startTime)
	task.EndTime = timeFromUnixMilli(endTime)
	task.RevokedAt = timeFromUnixMilli(revokedAt)
	task.CreatedAt = timeFromUnixMilli(createdAt)
	task.UpdatedAt = timeFromUnixMilli(updatedAt)
	task.CompletedAt = timeFromUnixMilli(completed)
	return &task, nil
}

type attachmentNodeRepository struct {
	store *Store
}

// AttachmentNodeRepository 返回附件节点注册表。
func (s *Store) AttachmentNodeRepository() repository.AttachmentNodeRepository {
	return &attachmentNodeRepository{store: s}
}

func (r *attachmentNodeRepository) Register(ctx context.Context, nodeID, instanceID, baseURL string, now time.Time) error {
	nowMilli := now.UnixMilli()
	_, err := r.store.db.ExecContext(ctx, r.store.stmts.UpsertAttachmentNode(),
		nodeID, instanceID, baseURL, nowMilli, nowMilli, now.Add(repository.AttachmentNodeTTL).UnixMilli())
	if err != nil {
		return fmt.Errorf("register attachment node %s: %w", nodeID, err)
	}
	return nil
}

func (r *attachmentNodeRepository) Heartbeat(ctx context.Context, nodeID, instanceID string, now time.Time) (bool, error) {
	nowMilli := now.UnixMilli()
	res, err := r.store.db.ExecContext(ctx,
		`UPDATE attachment_nodes SET last_seen_at = ?, expires_at = ?, status = 'online'
		 WHERE node_id = ? AND instance_id = ?`,
		nowMilli, now.Add(repository.AttachmentNodeTTL).UnixMilli(), nodeID, instanceID)
	if err != nil {
		return false, fmt.Errorf("heartbeat attachment node %s: %w", nodeID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected > 0 {
		return true, nil
	}
	var exists int
	err = r.store.db.QueryRowContext(ctx,
		`SELECT 1 FROM attachment_nodes WHERE node_id = ? AND instance_id = ?`, nodeID, instanceID).Scan(&exists)
	if err == stdsql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *attachmentNodeRepository) Deregister(ctx context.Context, nodeID, instanceID string, now time.Time) error {
	_, err := r.store.db.ExecContext(ctx,
		`UPDATE attachment_nodes SET status = 'offline', expires_at = ?
		 WHERE node_id = ? AND instance_id = ?`, now.UnixMilli(), nodeID, instanceID)
	if err != nil {
		return fmt.Errorf("deregister attachment node %s: %w", nodeID, err)
	}
	return nil
}

func (r *attachmentNodeRepository) Get(ctx context.Context, nodeID string) (*repository.AttachmentNode, error) {
	var (
		node      repository.AttachmentNode
		startedAt int64
		lastSeen  int64
		expiresAt int64
	)
	err := r.store.readDB.QueryRowContext(ctx,
		`SELECT node_id, instance_id, base_url, status, started_at, last_seen_at, expires_at
		 FROM attachment_nodes WHERE node_id = ?`, nodeID).
		Scan(&node.NodeID, &node.InstanceID, &node.BaseURL, &node.Status, &startedAt, &lastSeen, &expiresAt)
	if err == stdsql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get attachment node %s: %w", nodeID, err)
	}
	node.StartedAt = timeFromUnixMilli(startedAt)
	node.LastSeenAt = timeFromUnixMilli(lastSeen)
	node.ExpiresAt = timeFromUnixMilli(expiresAt)
	return &node, nil
}

func (r *attachmentNodeRepository) ListOnline(ctx context.Context, now time.Time) ([]*repository.AttachmentNode, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	rows, err := r.store.readDB.QueryContext(ctx,
		`SELECT node_id, instance_id, base_url, status, started_at, last_seen_at, expires_at
		 FROM attachment_nodes WHERE status = 'online' AND expires_at > ? ORDER BY node_id`, now.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("list attachment nodes: %w", err)
	}
	defer rows.Close()
	out := make([]*repository.AttachmentNode, 0)
	for rows.Next() {
		var (
			node      repository.AttachmentNode
			startedAt int64
			lastSeen  int64
			expiresAt int64
		)
		if scanErr := rows.Scan(&node.NodeID, &node.InstanceID, &node.BaseURL, &node.Status,
			&startedAt, &lastSeen, &expiresAt); scanErr != nil {
			return nil, scanErr
		}
		node.StartedAt = timeFromUnixMilli(startedAt)
		node.LastSeenAt = timeFromUnixMilli(lastSeen)
		node.ExpiresAt = timeFromUnixMilli(expiresAt)
		out = append(out, &node)
	}
	return out, rows.Err()
}

func (r *attachmentNodeRepository) Stats(ctx context.Context, now time.Time) (repository.AttachmentNodeStats, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	var stats repository.AttachmentNodeStats
	err := r.store.readDB.QueryRowContext(ctx,
		`SELECT
			COALESCE(SUM(CASE WHEN status = 'online' AND expires_at > ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'offline' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'online' AND expires_at <= ? THEN 1 ELSE 0 END), 0)
		 FROM attachment_nodes`, now.UnixMilli(), now.UnixMilli()).
		Scan(&stats.Online, &stats.Offline, &stats.Expired)
	if err != nil {
		return repository.AttachmentNodeStats{}, fmt.Errorf("attachment node stats: %w", err)
	}
	return stats, nil
}

var (
	_ repository.AttachmentFTPTaskRepository = (*attachmentFTPTaskRepository)(nil)
	_ repository.AttachmentNodeRepository    = (*attachmentNodeRepository)(nil)
)
