package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCoordinationStoreLeaseAndDedup(t *testing.T) {
	ctx := context.Background()
	backend := openTestSQLite(t)
	store := backend.CoordinationStore()

	ok, err := store.Acquire(ctx, "cleanup", "replica-a", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = store.Acquire(ctx, "cleanup", "replica-b", time.Minute)
	require.NoError(t, err)
	require.False(t, ok)

	// 同一 owner 可以续期。
	ok, err = store.Acquire(ctx, "cleanup", "replica-a", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	// 手动把租约改到过去，模拟到期；过期判定由数据库时间完成。
	expireLease := func(name string) {
		_, err := backend.DB().ExecContext(ctx,
			`UPDATE task_leases
			 SET expires_at = CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER) - 1000
			 WHERE name = ?`, name)
		require.NoError(t, err)
	}
	expireLease("cleanup")
	ok, err = store.Acquire(ctx, "cleanup", "replica-b", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	first, err := store.MarkOnce(ctx, "auto-9208:phone:alarm", time.Minute)
	require.NoError(t, err)
	require.True(t, first)
	second, err := store.MarkOnce(ctx, "auto-9208:phone:alarm", time.Minute)
	require.NoError(t, err)
	require.False(t, second)

	_, err = backend.DB().ExecContext(ctx,
		`UPDATE dedup_keys
		 SET expires_at = CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER) - 1000
		 WHERE dedup_key = ?`, "auto-9208:phone:alarm")
	require.NoError(t, err)
	third, err := store.MarkOnce(ctx, "auto-9208:phone:alarm", time.Minute)
	require.NoError(t, err)
	require.True(t, third)

	// 再让租约与去重同时过期，PruneExpired 应各删一行。
	expireLease("cleanup")
	_, err = backend.DB().ExecContext(ctx,
		`UPDATE dedup_keys
		 SET expires_at = CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER) - 1000
		 WHERE dedup_key = ?`, "auto-9208:phone:alarm")
	require.NoError(t, err)
	deleted, err := store.PruneExpired(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, deleted, int64(2))
}
