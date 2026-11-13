package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

func TestSessionDirectoryClaimIsIdempotentAndFences(t *testing.T) {
	ctx := context.Background()
	dir := openTestSQLite(t).SessionDirectoryRepository()

	ownerA := repository.SessionOwner{GatewayID: "gw-1", InstanceID: "inst-a", SessionID: "sess-1"}
	generation, err := dir.Claim(ctx, "13800000001", ownerA)
	require.NoError(t, err)
	require.Equal(t, uint64(1), generation)

	// 同一 owner 重复 claim 不递增。
	generation, err = dir.Claim(ctx, "13800000001", ownerA)
	require.NoError(t, err)
	require.Equal(t, uint64(1), generation)

	record, err := dir.Get(ctx, "13800000001")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, ownerA, record.SessionOwner)
	require.WithinDuration(t, time.Now(), record.ClaimedAt, time.Minute)

	// 换 session 递增。
	ownerB := repository.SessionOwner{GatewayID: "gw-1", InstanceID: "inst-a", SessionID: "sess-2"}
	generation, err = dir.Claim(ctx, "13800000001", ownerB)
	require.NoError(t, err)
	require.Equal(t, uint64(2), generation)

	// 旧 generation 与旧 owner 都被围栏。
	ok, err := dir.Validate(ctx, "13800000001", ownerA, 1)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = dir.Validate(ctx, "13800000001", ownerB, 2)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = dir.Validate(ctx, "13800000001", ownerB, 1)
	require.NoError(t, err)
	require.False(t, ok)

	// release 必须同时匹配 owner 与 generation。
	require.NoError(t, dir.Release(ctx, "13800000001", ownerA, 1))
	record, err = dir.Get(ctx, "13800000001")
	require.NoError(t, err)
	require.NotNil(t, record)

	require.NoError(t, dir.Release(ctx, "13800000001", ownerB, 2))
	record, err = dir.Get(ctx, "13800000001")
	require.NoError(t, err)
	require.Nil(t, record)

	// 目录行不存在时，任何非零 generation 都不通过。
	ok, err = dir.Validate(ctx, "13800000001", ownerB, 2)
	require.NoError(t, err)
	require.False(t, ok)
}
