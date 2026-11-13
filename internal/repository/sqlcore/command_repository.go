package sqlcore

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

const (
	commandSelectColumns = `SELECT command_id, idempotency_key, phone, msg_id, serial, payload, status, attempts, last_error,
		created_at, updated_at, last_attempt_at, next_attempt_at, expires_at, lease_owner, lease_expires_at
		FROM command_outbox`

	commandSerialSelectSQL = `SELECT next_serial FROM command_serial WHERE phone = ?`

	commandSerialUpdateSQL = `UPDATE command_serial SET next_serial = ?, updated_at = ? WHERE phone = ?`

	// 播种值是“已存在的最大 serial + 1”，与内存实现的 wrapSerial(max+1) 保持一致；
	// 直接取 MAX 会把已用过的流水号再分配一次。
	commandSerialSeedSQL = `SELECT COALESCE(MAX(serial), 0) + 1 FROM command_outbox WHERE phone = ?`

	commandSerialInFlightSQL = `SELECT COUNT(1) FROM command_outbox
		WHERE phone = ? AND msg_id = ? AND serial = ? AND status = 'pending'`
)

// commandPendingFilter 返回可投条件；时间比较一律用数据库当前时间。
func (r *commandRepository) commandPendingFilter() string {
	now := r.store.stmts.DBTimeMillisExpr()
	return ` status = 'pending'
		AND (next_attempt_at = 0 OR next_attempt_at <= ` + now + `)
		AND (expires_at = 0 OR expires_at > ` + now + `)
		AND (lease_expires_at = 0 OR lease_expires_at <= ` + now + `)`
}

func (r *commandRepository) commandCandidateSQL() string {
	return `SELECT command_id FROM command_outbox WHERE` + r.commandPendingFilter() +
		` ORDER BY next_attempt_at, created_at, command_id LIMIT ?`
}

func (r *commandRepository) commandDueListSQL() string {
	return commandSelectColumns + ` WHERE` + r.commandPendingFilter() +
		` ORDER BY next_attempt_at, created_at, command_id LIMIT ? OFFSET ?`
}

func (r *commandRepository) commandClaimSQL() string {
	now := r.store.stmts.DBTimeMillisExpr()
	return `UPDATE command_outbox
		SET lease_owner = ?, lease_expires_at = ` + now + ` + ?, updated_at = ` + now + `
		WHERE command_id = ? AND` + r.commandPendingFilter()
}

func (r *commandRepository) commandBeginAttemptSQL() string {
	now := r.store.stmts.DBTimeMillisExpr()
	return `UPDATE command_outbox
		SET attempts = attempts + 1, last_attempt_at = ` + now + `, updated_at = ` + now + `
		WHERE command_id = ? AND status = 'pending' AND lease_owner = ?
		AND (lease_expires_at = 0 OR lease_expires_at > ` + now + `)`
}

func (r *commandRepository) commandReleaseSQL() string {
	now := r.store.stmts.DBTimeMillisExpr()
	return `UPDATE command_outbox
		SET lease_owner = '', lease_expires_at = 0, next_attempt_at = ` + now + ` + ?, last_error = ?, updated_at = ` + now + `
		WHERE command_id = ? AND status = 'pending' AND lease_owner = ?`
}

func (r *commandRepository) commandReleaseOfflineSQL() string {
	now := r.store.stmts.DBTimeMillisExpr()
	return `UPDATE command_outbox
		SET lease_owner = '', lease_expires_at = 0, attempts = ?, last_attempt_at = ?, next_attempt_at = ` + now + ` + ?,
			last_error = ?, updated_at = ` + now + `
		WHERE command_id = ? AND status = 'pending' AND lease_owner = ?`
}

func (r *commandRepository) commandScheduleRetrySQL() string {
	now := r.store.stmts.DBTimeMillisExpr()
	return `UPDATE command_outbox
		SET lease_owner = '', lease_expires_at = 0, next_attempt_at = ` + now + ` + ?, last_error = ?, updated_at = ` + now + `
		WHERE phone = ? AND msg_id = ? AND serial = ? AND status = 'pending'`
}

func (r *commandRepository) commandExpireSQL() string {
	now := r.store.stmts.DBTimeMillisExpr()
	return `UPDATE command_outbox
		SET status = 'failed', last_error = ?, lease_owner = '', lease_expires_at = 0, updated_at = ` + now + `
		WHERE status = 'pending' AND expires_at > 0 AND expires_at <= ` + now
}

func (r *commandRepository) commandExpireByRoutingSQL() string {
	now := r.store.stmts.DBTimeMillisExpr()
	return `UPDATE command_outbox
		SET status = 'failed', last_error = ?, lease_owner = '', lease_expires_at = 0, updated_at = ` + now + `
		WHERE phone = ? AND msg_id = ? AND serial = ? AND status = 'pending'
		AND expires_at > 0 AND expires_at <= ` + now
}

func (r *commandRepository) commandMarkAckedSQL() string {
	now := r.store.stmts.DBTimeMillisExpr()
	return `UPDATE command_outbox
		SET status = 'acked', lease_owner = '', lease_expires_at = 0, updated_at = ` + now + `
		WHERE phone = ? AND msg_id = ? AND serial = ? AND status = 'pending'`
}

func (r *commandRepository) commandMarkFailedSQL() string {
	now := r.store.stmts.DBTimeMillisExpr()
	return `UPDATE command_outbox
		SET status = 'failed', last_error = ?, lease_owner = '', lease_expires_at = 0, updated_at = ` + now + `
		WHERE phone = ? AND msg_id = ? AND serial = ? AND status = 'pending'`
}

// commandRepository 是 repository.CommandRepository 的 SQL 实现。
type commandRepository struct {
	store *Store
}

// CommandRepository 返回下行命令 outbox 仓储。
func (s *Store) CommandRepository() repository.CommandRepository {
	return &commandRepository{store: s}
}

func (r *commandRepository) Enqueue(ctx context.Context, command *repository.DownlinkCommand, schedule repository.CommandSchedule) (*repository.DownlinkCommand, bool, error) {
	if command == nil {
		return nil, false, fmt.Errorf("enqueue command: nil command")
	}
	if command.CommandID == "" {
		return nil, false, fmt.Errorf("enqueue command: empty command id")
	}
	if command.IdempotencyKey == "" {
		return nil, false, fmt.Errorf("enqueue command: empty idempotency key")
	}
	attempts := command.Attempts
	if attempts == 0 {
		// 首次下发即第一次尝试，重投在此基础上累加。
		attempts = 1
	}
	stmt := r.store.stmts.InsertCommandIgnoringDuplicate()
	payload := command.Payload
	if payload == nil {
		payload = []byte{}
	}
	initialWait := schedule.InitialWait.Milliseconds()
	expireAfter := schedule.ExpireAfter.Milliseconds()
	result, err := r.store.db.ExecContext(ctx, stmt,
		command.CommandID, command.IdempotencyKey, command.Phone, int64(command.MsgID), int64(command.Serial),
		payload, string(repository.CommandStatusPending), attempts,
		unixMilliOrZero(command.LastAttemptAt), initialWait, expireAfter, expireAfter)
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
	ctx, cancel := readContext(ctx)
	defer cancel()
	row := r.store.readDB.QueryRowContext(ctx, commandSelectColumns+` WHERE command_id = ?`, commandID)
	command, err := scanCommand(row)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, repository.ErrCommandNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get command %s: %w", commandID, err)
	}
	return command, nil
}

func (r *commandRepository) FindByRouting(ctx context.Context, phone string, msgID, serial uint16) (*repository.DownlinkCommand, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	// 同一 (phone, msg_id, serial) 可能在回绕后出现多条记录：优先返回仍在途的
	// pending 命令，否则返回最近创建的一条，供迟到应答归类为 duplicate/late。
	row := r.store.readDB.QueryRowContext(ctx,
		commandSelectColumns+` WHERE phone = ? AND msg_id = ? AND serial = ?
		ORDER BY CASE WHEN status = 'pending' THEN 0 ELSE 1 END, created_at DESC LIMIT 1`,
		phone, int64(msgID), int64(serial))
	command, err := scanCommand(row)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, repository.ErrCommandNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find command %s/%04X/%d: %w", phone, msgID, serial, err)
	}
	return command, nil
}

func (r *commandRepository) getByIdempotencyKey(ctx context.Context, phone, key string) (*repository.DownlinkCommand, error) {
	// Enqueue 冲突后回读必须留在写池，保证整个 upsert 判定在写路径完成。
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

func (r *commandRepository) ListPending(ctx context.Context, query repository.PendingQuery) ([]*repository.DownlinkCommand, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	limit := query.Limit
	if limit <= 0 {
		// MySQL 不支持 LIMIT -1，用足够大的上限表达「不分页」。
		limit = int(^uint32(0) >> 1)
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	rows, err := r.store.readDB.QueryContext(ctx, r.commandDueListSQL(), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list pending commands: %w", err)
	}
	defer rows.Close()
	return scanCommands(rows)
}

func (r *commandRepository) PendingStats(ctx context.Context) (repository.PendingStats, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	now := r.store.stmts.DBTimeMillisExpr()
	var stats repository.PendingStats
	err := r.store.readDB.QueryRowContext(ctx,
		`SELECT COUNT(1), COALESCE(SUM(CASE WHEN expires_at > 0 AND expires_at <= `+now+` THEN 1 ELSE 0 END), 0)
		 FROM command_outbox WHERE status = 'pending'`).Scan(&stats.Pending, &stats.Expired)
	if err != nil {
		return repository.PendingStats{}, fmt.Errorf("command pending stats: %w", err)
	}
	return stats, nil
}

func (r *commandRepository) ClaimPending(ctx context.Context, claim repository.CommandClaim) ([]*repository.DownlinkCommand, error) {
	if claim.Owner == "" {
		return nil, fmt.Errorf("claim commands: empty owner")
	}
	if claim.Lease <= 0 {
		return nil, fmt.Errorf("claim commands: lease must be positive")
	}
	if claim.Limit <= 0 {
		return nil, nil
	}
	rows, err := r.store.db.QueryContext(ctx, r.commandCandidateSQL(), claim.Limit)
	if err != nil {
		return nil, fmt.Errorf("list claimable commands: %w", err)
	}
	ids := make([]string, 0, claim.Limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan claimable command: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate claimable commands: %w", err)
	}
	rows.Close()

	// 逐条条件更新：只有 lease 仍为空或已失效的实例能占用成功。
	// 多实例同时选中同一条命令时，后提交的更新看到新租约，affected=0 自动跳过。
	leaseMilli := claim.Lease.Milliseconds()
	claimed := make([]*repository.DownlinkCommand, 0, len(ids))
	for _, id := range ids {
		result, err := r.store.db.ExecContext(ctx, r.commandClaimSQL(),
			claim.Owner, leaseMilli, id)
		if err != nil {
			return nil, fmt.Errorf("claim command %s: %w", id, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("claim command %s rows affected: %w", id, err)
		}
		if affected == 0 {
			continue
		}
		command, err := r.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		claimed = append(claimed, command)
	}
	return claimed, nil
}

func (r *commandRepository) BeginAttempt(ctx context.Context, commandID, owner string) (bool, error) {
	result, err := r.store.db.ExecContext(ctx, r.commandBeginAttemptSQL(), commandID, owner)
	if err != nil {
		return false, fmt.Errorf("begin command attempt %s: %w", commandID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("begin command attempt %s rows affected: %w", commandID, err)
	}
	return affected > 0, nil
}

func (r *commandRepository) ReleaseAttempt(ctx context.Context, commandID, owner string, delay time.Duration, reason string) (bool, error) {
	result, err := r.store.db.ExecContext(ctx, r.commandReleaseSQL(),
		delay.Milliseconds(), reason, commandID, owner)
	if err != nil {
		return false, fmt.Errorf("release command %s: %w", commandID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("release command %s rows affected: %w", commandID, err)
	}
	return affected > 0, nil
}

func (r *commandRepository) ReleaseOffline(ctx context.Context, commandID, owner string, attempts int, lastAttemptAt time.Time, delay time.Duration, reason string) (bool, error) {
	result, err := r.store.db.ExecContext(ctx, r.commandReleaseOfflineSQL(),
		attempts, unixMilliOrZero(lastAttemptAt), delay.Milliseconds(), reason, commandID, owner)
	if err != nil {
		return false, fmt.Errorf("release offline command %s: %w", commandID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("release offline command %s rows affected: %w", commandID, err)
	}
	return affected > 0, nil
}

// NextSerial 在单个事务内完成“读取游标 → 跳过在途组合 → 写回游标”。
//
// MySQL 用 SELECT ... FOR UPDATE 锁住该 phone 的游标行；SQLite 是单写者模型且
// 连接池限定单连接，事务本身即串行化。播种值取该 phone 已存在的最大 serial + 1，
// 保证重启或多实例接手时不会从 0 重新开始。
func (r *commandRepository) NextSerial(ctx context.Context, phone string, msgID uint16) (uint16, error) {
	if phone == "" {
		return 0, fmt.Errorf("next serial: empty phone")
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("next serial: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UnixMilli()
	insertStmt := r.store.stmts.EnsureCommandSerialRow()
	lockClause := r.store.stmts.LockCommandSerialRow()
	var seed int64
	if err := tx.QueryRowContext(ctx, commandSerialSeedSQL, phone).Scan(&seed); err != nil {
		return 0, fmt.Errorf("next serial: read max serial: %w", err)
	}
	if seed < 1 || seed > 65535 {
		seed = 1
	}
	if _, err := tx.ExecContext(ctx, insertStmt, phone, seed, now); err != nil {
		return 0, fmt.Errorf("next serial: seed cursor: %w", err)
	}

	var next int64
	if err := tx.QueryRowContext(ctx, commandSerialSelectSQL+lockClause, phone).Scan(&next); err != nil {
		return 0, fmt.Errorf("next serial: read cursor: %w", err)
	}
	for i := 0; i < 65535; i++ {
		candidate := normalizeSerial(next)
		var inFlight int64
		if err := tx.QueryRowContext(ctx, commandSerialInFlightSQL,
			phone, int64(msgID), int64(candidate)).Scan(&inFlight); err != nil {
			return 0, fmt.Errorf("next serial: check in-flight %s/%04X/%d: %w", phone, msgID, candidate, err)
		}
		if inFlight == 0 {
			if _, err := tx.ExecContext(ctx, commandSerialUpdateSQL,
				int64(normalizeSerial(int64(candidate)+1)), now, phone); err != nil {
				return 0, fmt.Errorf("next serial: update cursor: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return 0, fmt.Errorf("next serial: commit: %w", err)
			}
			return candidate, nil
		}
		next = int64(normalizeSerial(int64(candidate) + 1))
	}
	return 0, fmt.Errorf("next serial: no free serial for %s/%04X", phone, msgID)
}

// normalizeSerial 把游标限制在 1..65535，0 是协议保留值。
func normalizeSerial(value int64) uint16 {
	for value < 1 {
		value += 65535
	}
	value = (value-1)%65535 + 1
	return uint16(value)
}

func (r *commandRepository) ScheduleRetry(ctx context.Context, phone string, msgID, serial uint16, delay time.Duration, reason string) (bool, error) {
	result, err := r.store.db.ExecContext(ctx, r.commandScheduleRetrySQL(),
		delay.Milliseconds(), reason, phone, int64(msgID), int64(serial))
	if err != nil {
		return false, fmt.Errorf("schedule command retry %s/%04X/%d: %w", phone, msgID, serial, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("schedule command retry rows affected %s/%04X/%d: %w", phone, msgID, serial, err)
	}
	return affected > 0, nil
}

func (r *commandRepository) ExpirePending(ctx context.Context, reason string) (int, error) {
	result, err := r.store.db.ExecContext(ctx, r.commandExpireSQL(), reason)
	if err != nil {
		return 0, fmt.Errorf("expire pending commands: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("expire pending commands rows affected: %w", err)
	}
	return int(affected), nil
}

func (r *commandRepository) ExpireByRouting(ctx context.Context, phone string, msgID, serial uint16, reason string) (bool, error) {
	result, err := r.store.db.ExecContext(ctx, r.commandExpireByRoutingSQL(), reason, phone, int64(msgID), int64(serial))
	if err != nil {
		return false, fmt.Errorf("expire command by routing %s/%04X/%d: %w", phone, msgID, serial, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("expire command by routing rows affected %s/%04X/%d: %w", phone, msgID, serial, err)
	}
	return affected > 0, nil
}

func (r *commandRepository) MarkAcked(ctx context.Context, phone string, msgID, serial uint16) (bool, error) {
	return r.finish(ctx, r.commandMarkAckedSQL(), phone, msgID, serial)
}

func (r *commandRepository) MarkFailed(ctx context.Context, phone string, msgID, serial uint16, reason string) (bool, error) {
	result, err := r.store.db.ExecContext(ctx, r.commandMarkFailedSQL(), reason, phone, int64(msgID), int64(serial))
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
	result, err := r.store.db.ExecContext(ctx, stmt, phone, int64(msgID), int64(serial))
	if err != nil {
		return false, fmt.Errorf("mark command acked %s/%04X/%d: %w", phone, msgID, serial, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("mark command acked rows affected %s/%04X/%d: %w", phone, msgID, serial, err)
	}
	return affected > 0, nil
}

func scanCommands(rows *stdsql.Rows) ([]*repository.DownlinkCommand, error) {
	commands := make([]*repository.DownlinkCommand, 0)
	for rows.Next() {
		command, err := scanCommand(rows)
		if err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return commands, nil
}

// rowScanner 抽象 *sql.Row 与 *sql.Rows，两者 Scan 签名一致。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCommand(row rowScanner) (*repository.DownlinkCommand, error) {
	var (
		command        repository.DownlinkCommand
		msgID          int64
		serial         int64
		status         string
		attempts       int64
		createdAt      int64
		updatedAt      int64
		lastAttemptAt  int64
		nextAttemptAt  int64
		expiresAt      int64
		leaseExpiresAt int64
	)
	if err := row.Scan(
		&command.CommandID, &command.IdempotencyKey, &command.Phone, &msgID, &serial, &command.Payload,
		&status, &attempts, &command.LastError, &createdAt, &updatedAt,
		&lastAttemptAt, &nextAttemptAt, &expiresAt, &command.LeaseOwner, &leaseExpiresAt,
	); err != nil {
		return nil, err
	}
	command.MsgID = uint16(msgID)
	command.Serial = uint16(serial)
	command.Status = repository.CommandStatus(status)
	command.Attempts = int(attempts)
	command.CreatedAt = time.UnixMilli(createdAt)
	command.UpdatedAt = time.UnixMilli(updatedAt)
	command.LastAttemptAt = timeFromUnixMilli(lastAttemptAt)
	command.NextAttemptAt = timeFromUnixMilli(nextAttemptAt)
	command.ExpiresAt = timeFromUnixMilli(expiresAt)
	command.LeaseExpiresAt = timeFromUnixMilli(leaseExpiresAt)
	return &command, nil
}

// unixMilliOrZero 把零值时间写成 0，避免零值被编码成负数导致语义歧义。
func unixMilliOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// timeFromUnixMilli 把 0 还原为零值时间，与 unixMilliOrZero 对称。
func timeFromUnixMilli(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.UnixMilli(value)
}

var _ repository.CommandRepository = (*commandRepository)(nil)
