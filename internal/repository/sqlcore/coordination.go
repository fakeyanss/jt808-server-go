package sqlcore

import (
	"context"
	"fmt"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/coordination"
)

// coordinationStore 用 task_leases / dedup_keys 两张表实现跨副本协调。
// 所有写操作都通过条件 UPDATE 完成，依赖数据库的行锁保证同一行只有一个赢家。
type coordinationStore struct {
	store *Store
}

// CoordinationStore 返回基于当前 SQL 连接池的协调存储。
func (s *Store) CoordinationStore() coordination.Store {
	return &coordinationStore{store: s}
}

func (c *coordinationStore) Acquire(ctx context.Context, name, owner string, ttl time.Duration) (bool, error) {
	now := c.store.stmts.DBTimeMillisExpr()
	if _, err := c.store.db.ExecContext(ctx, c.store.stmts.EnsureTaskLeaseRow(), name); err != nil {
		return false, err
	}
	res, err := c.store.db.ExecContext(ctx, fmt.Sprintf(
		`UPDATE task_leases SET owner = ?, expires_at = %s + ?, updated_at = %s
		 WHERE name = ? AND (expires_at <= %s OR owner = ?)`, now, now, now),
		owner, normalizeTTL(ttl).Milliseconds(), name, owner)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (c *coordinationStore) Release(ctx context.Context, name, owner string) error {
	_, err := c.store.db.ExecContext(ctx,
		`DELETE FROM task_leases WHERE name = ? AND owner = ?`, name, owner)
	return err
}

func (c *coordinationStore) MarkOnce(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	now := c.store.stmts.DBTimeMillisExpr()
	if _, err := c.store.db.ExecContext(ctx, c.store.stmts.EnsureDedupKeyRow(), key); err != nil {
		return false, err
	}
	res, err := c.store.db.ExecContext(ctx, fmt.Sprintf(
		`UPDATE dedup_keys SET expires_at = %s + ?, updated_at = %s
		 WHERE dedup_key = ? AND expires_at <= %s`, now, now, now),
		normalizeTTL(ttl).Milliseconds(), key)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (c *coordinationStore) PruneExpired(ctx context.Context) (int64, error) {
	now := c.store.stmts.DBTimeMillisExpr()
	res, err := c.store.db.ExecContext(ctx, `DELETE FROM task_leases WHERE expires_at <= `+now)
	if err != nil {
		return 0, err
	}
	leases, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	res, err = c.store.db.ExecContext(ctx, `DELETE FROM dedup_keys WHERE expires_at <= `+now)
	if err != nil {
		return leases, err
	}
	dedup, err := res.RowsAffected()
	if err != nil {
		return leases, err
	}
	return leases + dedup, nil
}

func normalizeTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return coordination.DefaultTTL
	}
	return ttl
}
