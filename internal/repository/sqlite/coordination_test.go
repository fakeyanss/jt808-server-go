package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCoordinationStoreLeaseAndDedup(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t).CoordinationStore()
	now := time.Unix(1_800_000_000, 0)

	ok, err := store.Acquire(ctx, "cleanup", "replica-a", now, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = store.Acquire(ctx, "cleanup", "replica-b", now, time.Minute)
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = store.Acquire(ctx, "cleanup", "replica-a", now.Add(30*time.Second), time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = store.Acquire(ctx, "cleanup", "replica-b", now.Add(2*time.Minute), time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	first, err := store.MarkOnce(ctx, "auto-9208:phone:alarm", now, time.Minute)
	require.NoError(t, err)
	require.True(t, first)
	second, err := store.MarkOnce(ctx, "auto-9208:phone:alarm", now.Add(30*time.Second), time.Minute)
	require.NoError(t, err)
	require.False(t, second)
	third, err := store.MarkOnce(ctx, "auto-9208:phone:alarm", now.Add(2*time.Minute), time.Minute)
	require.NoError(t, err)
	require.True(t, third)

	deleted, err := store.PruneExpired(ctx, now.Add(3*time.Minute))
	require.NoError(t, err)
	require.GreaterOrEqual(t, deleted, int64(2))
}
