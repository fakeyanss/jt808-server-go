package sqlcore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

type gatewayNodeRepository struct {
	store *Store
}

// GatewayNodeRepository 返回基于当前 SQL 连接池的节点注册表。
func (s *Store) GatewayNodeRepository() repository.GatewayNodeRepository {
	return &gatewayNodeRepository{store: s}
}

func (r *gatewayNodeRepository) Register(ctx context.Context, gatewayID, instanceID, downlinkURL string) error {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// 新实例注册即顶替旧实例：同一事务内清掉旧 instance 的目录行，
	// 避免旧会话在下行路由里继续指向已经消失的进程。
	if _, err = tx.ExecContext(ctx, r.store.stmts.DeleteStaleSessionDirectory(),
		gatewayID, instanceID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, r.store.stmts.UpsertGatewayNode(),
		gatewayID, instanceID, downlinkURL); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *gatewayNodeRepository) Heartbeat(ctx context.Context, gatewayID, instanceID string) (bool, error) {
	res, err := r.store.db.ExecContext(ctx, r.store.stmts.HeartbeatGatewayNode(), gatewayID, instanceID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected > 0 {
		return true, nil
	}
	// 同一毫秒内重复心跳时 MySQL 可能返回 0 行受影响，用一次存在性查询兜底。
	var exists int
	err = r.store.db.QueryRowContext(ctx,
		`SELECT 1 FROM gateway_nodes WHERE gateway_id = ? AND instance_id = ?`,
		gatewayID, instanceID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *gatewayNodeRepository) Deregister(ctx context.Context, gatewayID, instanceID string) error {
	_, err := r.store.db.ExecContext(ctx, r.store.stmts.DeregisterGatewayNode(), gatewayID, instanceID)
	return err
}

func (r *gatewayNodeRepository) Get(ctx context.Context, gatewayID string) (*repository.GatewayNode, error) {
	var (
		node      repository.GatewayNode
		startedAt any
		lastSeen  any
		expiresAt any
	)
	err := r.store.db.QueryRowContext(ctx, r.store.stmts.SelectGatewayNode(), gatewayID).Scan(
		&node.GatewayID, &node.InstanceID, &node.DownlinkURL, &node.Status,
		&startedAt, &lastSeen, &expiresAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	node.StartedAt, err = parseDBTime(startedAt)
	if err != nil {
		return nil, err
	}
	node.LastSeenAt, err = parseDBTime(lastSeen)
	if err != nil {
		return nil, err
	}
	node.ExpiresAt, err = parseDBTime(expiresAt)
	if err != nil {
		return nil, err
	}
	return &node, nil
}

func (r *gatewayNodeRepository) Stats(ctx context.Context) (repository.GatewayNodeStats, error) {
	var stats repository.GatewayNodeStats
	err := r.store.readDB.QueryRowContext(ctx, r.store.stmts.SelectGatewayNodeStats()).
		Scan(&stats.Online, &stats.Offline, &stats.Expired)
	if err != nil {
		return repository.GatewayNodeStats{}, err
	}
	return stats, nil
}

func parseDBTime(value any) (time.Time, error) {
	switch v := value.(type) {
	case time.Time:
		return v, nil
	case string:
		return parseDBTimeString(v)
	case []byte:
		return parseDBTimeString(string(v))
	default:
		return time.Time{}, fmt.Errorf("unsupported database time type %T", value)
	}
}

func parseDBTimeString(value string) (time.Time, error) {
	for _, layout := range []string{
		"2006-01-02 15:04:05.000",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported database time value %q", value)
}
