package coordination

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeClock 让内存后端按测试推进的时间判定租约与去重过期。
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func TestMemoryStoreLeaseIsExclusiveAndExpires(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	store := NewMemoryStoreWithClock(clock.Now)

	ok, err := store.Acquire(ctx, "cleanup", "replica-a", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = store.Acquire(ctx, "cleanup", "replica-b", time.Minute)
	require.NoError(t, err)
	require.False(t, ok)

	// 同一 owner 可以续期。
	clock.Advance(30 * time.Second)
	ok, err = store.Acquire(ctx, "cleanup", "replica-a", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	// 过期后其它副本可以接手。
	clock.Advance(2 * time.Minute)
	ok, err = store.Acquire(ctx, "cleanup", "replica-b", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestMemoryStoreMarkOnceHonorsTTL(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	store := NewMemoryStoreWithClock(clock.Now)

	first, err := store.MarkOnce(ctx, "auto-9208:phone:alarm", time.Minute)
	require.NoError(t, err)
	require.True(t, first)

	clock.Advance(30 * time.Second)
	second, err := store.MarkOnce(ctx, "auto-9208:phone:alarm", time.Minute)
	require.NoError(t, err)
	require.False(t, second)

	clock.Advance(2 * time.Minute)
	third, err := store.MarkOnce(ctx, "auto-9208:phone:alarm", time.Minute)
	require.NoError(t, err)
	require.True(t, third)
}

func TestRunnerSkipsWhenAnotherReplicaHoldsLease(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	store := NewMemoryStoreWithClock(clock.Now)
	ok, err := store.Acquire(ctx, "cleanup", "other-replica", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	runner := NewRunner(store, time.Minute)
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
