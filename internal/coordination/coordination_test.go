package coordination

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type staticClock struct{ now time.Time }

func (c staticClock) Now() time.Time { return c.now }

func TestMemoryStoreLeaseIsExclusiveAndExpires(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	now := time.Unix(1_800_000_000, 0)

	ok, err := store.Acquire(ctx, "cleanup", "replica-a", now, time.Minute, DefaultLeaseTolerance)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = store.Acquire(ctx, "cleanup", "replica-b", now, time.Minute, DefaultLeaseTolerance)
	require.NoError(t, err)
	require.False(t, ok)

	// 同一 owner 可以续期。
	ok, err = store.Acquire(ctx, "cleanup", "replica-a", now.Add(30*time.Second), time.Minute, DefaultLeaseTolerance)
	require.NoError(t, err)
	require.True(t, ok)

	// 到期 + 容差之后其它副本才能接手。
	ok, err = store.Acquire(ctx, "cleanup", "replica-b",
		now.Add(time.Minute+DefaultLeaseTolerance-time.Millisecond), time.Minute, DefaultLeaseTolerance)
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = store.Acquire(ctx, "cleanup", "replica-b", now.Add(2*time.Minute), time.Minute, DefaultLeaseTolerance)
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
	ok, err := store.Acquire(ctx, "cleanup", "other-replica", now, time.Minute, DefaultLeaseTolerance)
	require.NoError(t, err)
	require.True(t, ok)

	runner := NewRunner(store, time.Minute, staticClock{now: now}, DefaultLeaseTolerance)
	runner.owner = "this-replica"
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
