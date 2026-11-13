package sqlcore

import (
	"context"
	"database/sql"
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

func (r *gatewayNodeRepository) Register(ctx context.Context, gatewayID, instanceID, downlinkURL string, now time.Time) error {
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
	nowMilli := now.UnixMilli()
	expiresAt := now.Add(repository.GatewayNodeTTL).UnixMilli()
	if _, err = tx.ExecContext(ctx, r.store.stmts.UpsertGatewayNode(),
		gatewayID, instanceID, downlinkURL, nowMilli, nowMilli, expiresAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *gatewayNodeRepository) Heartbeat(ctx context.Context, gatewayID, instanceID string, now time.Time) (bool, error) {
	nowMilli := now.UnixMilli()
	expiresAt := now.Add(repository.GatewayNodeTTL).UnixMilli()
	res, err := r.store.db.ExecContext(ctx, r.store.stmts.HeartbeatGatewayNode(),
		nowMilli, expiresAt, gatewayID, instanceID)
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
	// 写入与更新落在同一毫秒时 MySQL 可能返回 0 行受影响，用一次存在性查询兜底。
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

func (r *gatewayNodeRepository) Deregister(ctx context.Context, gatewayID, instanceID string, now time.Time) error {
	_, err := r.store.db.ExecContext(ctx, r.store.stmts.DeregisterGatewayNode(),
		now.UnixMilli(), gatewayID, instanceID)
	return err
}

func (r *gatewayNodeRepository) Get(ctx context.Context, gatewayID string) (*repository.GatewayNode, error) {
	var (
		node      repository.GatewayNode
		startedAt int64
		lastSeen  int64
		expiresAt int64
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
	node.StartedAt = timeFromUnixMilli(startedAt)
	node.LastSeenAt = timeFromUnixMilli(lastSeen)
	node.ExpiresAt = timeFromUnixMilli(expiresAt)
	return &node, nil
}

func (r *gatewayNodeRepository) Stats(ctx context.Context, now time.Time) (repository.GatewayNodeStats, error) {
	var stats repository.GatewayNodeStats
	nowMilli := now.UnixMilli()
	err := r.store.readDB.QueryRowContext(ctx, r.store.stmts.SelectGatewayNodeStats(), nowMilli, nowMilli).
		Scan(&stats.Online, &stats.Offline, &stats.Expired)
	if err != nil {
		return repository.GatewayNodeStats{}, err
	}
	return stats, nil
}

var _ repository.GatewayNodeRepository = (*gatewayNodeRepository)(nil)
