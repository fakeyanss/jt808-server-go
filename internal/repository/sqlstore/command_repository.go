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
	commandInsertSQLite = `INSERT INTO command_outbox
		(command_id, idempotency_key, phone, msg_id, serial, payload, status, attempts, last_error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(phone, idempotency_key) DO NOTHING`

	commandInsertMySQL = `INSERT IGNORE INTO command_outbox
		(command_id, idempotency_key, phone, msg_id, serial, payload, status, attempts, last_error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	commandSelectColumns = `SELECT command_id, idempotency_key, phone, msg_id, serial, payload, status, attempts, last_error, created_at, updated_at FROM command_outbox`

	commandMarkAckedSQL = `UPDATE command_outbox
		SET status = 'acked', updated_at = ?
		WHERE phone = ? AND msg_id = ? AND serial = ? AND status = 'pending'`

	commandMarkFailedSQL = `UPDATE command_outbox
		SET status = 'failed', last_error = ?, updated_at = ?
		WHERE phone = ? AND msg_id = ? AND serial = ? AND status = 'pending'`
)

// commandRepository 是 repository.CommandRepository 的 SQL 实现。
type commandRepository struct {
	store *Store
}

// CommandRepository 返回下行命令 outbox 仓储。
func (s *Store) CommandRepository() repository.CommandRepository {
	return &commandRepository{store: s}
}

func (r *commandRepository) Enqueue(ctx context.Context, command *repository.DownlinkCommand) (*repository.DownlinkCommand, bool, error) {
	if command == nil {
		return nil, false, fmt.Errorf("enqueue command: nil command")
	}
	if command.CommandID == "" {
		return nil, false, fmt.Errorf("enqueue command: empty command id")
	}
	if command.IdempotencyKey == "" {
		return nil, false, fmt.Errorf("enqueue command: empty idempotency key")
	}
	now := time.Now()
	createdAt := command.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	attempts := command.Attempts
	if attempts == 0 {
		attempts = 1
	}
	stmt := commandInsertSQLite
	if r.store.backend == BackendMySQL {
		stmt = commandInsertMySQL
	}
	payload := command.Payload
	if payload == nil {
		payload = []byte{}
	}
	result, err := r.store.db.ExecContext(ctx, stmt,
		command.CommandID, command.IdempotencyKey, command.Phone, int64(command.MsgID), int64(command.Serial),
		payload, string(repository.CommandStatusPending), attempts, "", createdAt.UnixMilli(), now.UnixMilli())
	if err != nil {
		return nil, false, fmt.Errorf("enqueue command %s: %w", command.CommandID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, false, fmt.Errorf("enqueue command %s rows affected: %w", command.CommandID, err)
	}
	if affected == 0 {
		existing, err := r.getByIdempotencyKey(ctx, command.Phone, command.IdempotencyKey)
		if err != nil {
			return nil, false, err
		}
		if existing == nil {
			return nil, false, fmt.Errorf("enqueue command %s: duplicate key without stored row", command.CommandID)
		}
		return existing, false, nil
	}
	stored, err := r.Get(ctx, command.CommandID)
	if err != nil {
		return nil, false, err
	}
	return stored, true, nil
}

func (r *commandRepository) Get(ctx context.Context, commandID string) (*repository.DownlinkCommand, error) {
	row := r.store.db.QueryRowContext(ctx, commandSelectColumns+` WHERE command_id = ?`, commandID)
	command, err := scanCommand(row)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, repository.ErrCommandNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get command %s: %w", commandID, err)
	}
	return command, nil
}

func (r *commandRepository) getByIdempotencyKey(ctx context.Context, phone, key string) (*repository.DownlinkCommand, error) {
	row := r.store.db.QueryRowContext(ctx, commandSelectColumns+` WHERE phone = ? AND idempotency_key = ?`, phone, key)
	command, err := scanCommand(row)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get command by idempotency key %s/%s: %w", phone, key, err)
	}
	return command, nil
}

func (r *commandRepository) MarkAcked(ctx context.Context, phone string, msgID, serial uint16) (bool, error) {
	return r.finish(ctx, commandMarkAckedSQL, phone, msgID, serial)
}

func (r *commandRepository) MarkFailed(ctx context.Context, phone string, msgID, serial uint16, reason string) (bool, error) {
	result, err := r.store.db.ExecContext(ctx, commandMarkFailedSQL, reason, time.Now().UnixMilli(), phone, int64(msgID), int64(serial))
	if err != nil {
		return false, fmt.Errorf("mark command failed %s/%04X/%d: %w", phone, msgID, serial, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("mark command failed rows affected %s/%04X/%d: %w", phone, msgID, serial, err)
	}
	return affected > 0, nil
}

func (r *commandRepository) finish(ctx context.Context, stmt, phone string, msgID, serial uint16) (bool, error) {
	result, err := r.store.db.ExecContext(ctx, stmt, time.Now().UnixMilli(), phone, int64(msgID), int64(serial))
	if err != nil {
		return false, fmt.Errorf("mark command acked %s/%04X/%d: %w", phone, msgID, serial, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("mark command acked rows affected %s/%04X/%d: %w", phone, msgID, serial, err)
	}
	return affected > 0, nil
}

func scanCommand(row *stdsql.Row) (*repository.DownlinkCommand, error) {
	var (
		command   repository.DownlinkCommand
		msgID     int64
		serial    int64
		status    string
		attempts  int64
		createdAt int64
		updatedAt int64
	)
	if err := row.Scan(
		&command.CommandID, &command.IdempotencyKey, &command.Phone, &msgID, &serial, &command.Payload,
		&status, &attempts, &command.LastError, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	command.MsgID = uint16(msgID)
	command.Serial = uint16(serial)
	command.Status = repository.CommandStatus(status)
	command.Attempts = int(attempts)
	command.CreatedAt = time.UnixMilli(createdAt)
	command.UpdatedAt = time.UnixMilli(updatedAt)
	return &command, nil
}

var _ repository.CommandRepository = (*commandRepository)(nil)
