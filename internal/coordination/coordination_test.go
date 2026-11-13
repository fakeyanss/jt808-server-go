package coordination

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMemoryStoreLeaseIsExclusiveAndExpires(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	now := time.Unix(1_800_000_000, 0)

	ok, err := store.Acquire(ctx, "cleanup", "replica-a", now, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = store.Acquire(ctx, "cleanup", "replica-b", now, time.Minute)
	require.NoError(t, err)
	require.False(t, ok)

	// 同一 owner 可以续期。
	ok, err = store.Acquire(ctx, "cleanup", "replica-a", now.Add(30*time.Second), time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	// 过期后其它副本可以接手。
	ok, err = store.Acquire(ctx, "cleanup", "replica-b", now.Add(2*time.Minute), time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestMemoryStoreMarkOnceHonorsTTL(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	now := time.Unix(1_800_000_000, 0)

	first, err := store.MarkOnce(ctx, "auto-9208:phone:alarm", now, time.Minute)
	require.NoError(t, err)
	require.True(t, first)

	second, err := store.MarkOnce(ctx, "auto-9208:phone:alarm", now.Add(30*time.Second), time.Minute)
	require.NoError(t, err)
	require.False(t, second)

	third, err := store.MarkOnce(ctx, "auto-9208:phone:alarm", now.Add(2*time.Minute), time.Minute)
	require.NoError(t, err)
	require.True(t, third)
}

func TestRunnerSkipsWhenAnotherReplicaHoldsLease(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	now := time.Unix(1_800_000_000, 0)
	ok, err := store.Acquire(ctx, "cleanup", "other-replica", now, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	runner := NewRunner(store, time.Minute)
	runner.owner = "this-replica"
	runner.now = func() time.Time { return now }
	called := false
	require.False(t, runner.Run(ctx, "cleanup", func(context.Context) {
		called = true
	}))
	require.False(t, called)

	require.NoError(t, store.Release(ctx, "cleanup", "other-replica"))
	require.True(t, runner.Run(ctx, "cleanup", func(context.Context) {
		called = true
	}))
	require.True(t, called)
}
