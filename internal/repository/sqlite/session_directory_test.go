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

// Route 的有效性判定全部在 SQL 内完成：目录存在、节点在线且未过期。
func TestSessionDirectoryRouteFollowsNodeLiveness(t *testing.T) {
	ctx := context.Background()
	backend := openTestSQLite(t)
	dir := backend.SessionDirectoryRepository()
	nodes := backend.GatewayNodeRepository()

	const phone = "13800000002"
	owner := repository.SessionOwner{GatewayID: "gw-1", InstanceID: "inst-a", SessionID: "sess-1"}

	// 没有目录行。
	url, ok, err := dir.Route(ctx, phone, "")
	require.NoError(t, err)
	require.False(t, ok)
	require.Empty(t, url)

	_, err = dir.Claim(ctx, phone, owner)
	require.NoError(t, err)

	// 目录存在但节点未注册，不可路由。
	_, ok, err = dir.Route(ctx, phone, "")
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, nodes.Register(ctx, "gw-1", "inst-a", "http://gw-1:9100"))
	url, ok, err = dir.Route(ctx, phone, "")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "http://gw-1:9100", url)

	// SessionID 显式指定时必须与目录一致。
	url, ok, err = dir.Route(ctx, phone, "sess-1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "http://gw-1:9100", url)

	_, ok, err = dir.Route(ctx, phone, "sess-stale")
	require.NoError(t, err)
	require.False(t, ok)

	// 节点注销后立即不可路由。
	require.NoError(t, nodes.Deregister(ctx, "gw-1", "inst-a"))
	_, ok, err = dir.Route(ctx, phone, "")
	require.NoError(t, err)
	require.False(t, ok)

	// 节点过期后不可路由；过期时间由数据库决定。
	require.NoError(t, nodes.Register(ctx, "gw-1", "inst-a", "http://gw-1:9100"))
	_, err = backend.DB().ExecContext(ctx,
		`UPDATE gateway_nodes SET expires_at = strftime('%Y-%m-%d %H:%M:%f','now','-1 seconds')
		 WHERE gateway_id = ?`, "gw-1")
	require.NoError(t, err)
	_, ok, err = dir.Route(ctx, phone, "")
	require.NoError(t, err)
	require.False(t, ok)

	// 目录指向的实例与节点当前实例不一致时不可路由。
	require.NoError(t, nodes.Register(ctx, "gw-1", "inst-b", "http://gw-1:9100"))
	_, ok, err = dir.Route(ctx, phone, "")
	require.NoError(t, err)
	require.False(t, ok)
}
