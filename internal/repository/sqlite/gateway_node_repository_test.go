package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

func TestGatewayNodeRepositoryLifecycle(t *testing.T) {
	ctx := context.Background()
	backend := openTestSQLite(t)
	repo := backend.GatewayNodeRepository()

	require.NoError(t, repo.Register(ctx, "gw-1", "inst-a", "http://gw-1:9001"))

	node, err := repo.Get(ctx, "gw-1")
	require.NoError(t, err)
	require.NotNil(t, node)
	require.Equal(t, "inst-a", node.InstanceID)
	require.Equal(t, "http://gw-1:9001", node.DownlinkURL)
	require.Equal(t, "online", node.Status)
	require.WithinDuration(t, time.Now(), node.StartedAt, time.Minute)
	require.WithinDuration(t, time.Now().Add(30*time.Second), node.ExpiresAt, time.Minute)

	ok, err := repo.Heartbeat(ctx, "gw-1", "inst-a")
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = repo.Heartbeat(ctx, "gw-1", "inst-stale")
	require.NoError(t, err)
	require.False(t, ok)

	// 新实例注册后，旧实例心跳必须被围栏，且旧实例的会话目录行被清理。
	dir := backend.SessionDirectoryRepository()
	_, err = dir.Claim(ctx, "13800000003", repository.SessionOwner{
		GatewayID: "gw-1", InstanceID: "inst-a", SessionID: "sess-1",
	})
	require.NoError(t, err)
	require.NoError(t, repo.Register(ctx, "gw-1", "inst-b", "http://gw-1:9001"))
	record, err := dir.Get(ctx, "13800000003")
	require.NoError(t, err)
	require.Nil(t, record)
	ok, err = repo.Heartbeat(ctx, "gw-1", "inst-a")
	require.NoError(t, err)
	require.False(t, ok)

	node, err = repo.Get(ctx, "gw-1")
	require.NoError(t, err)
	require.Equal(t, "inst-b", node.InstanceID)

	require.NoError(t, repo.Deregister(ctx, "gw-1", "inst-b"))
	node, err = repo.Get(ctx, "gw-1")
	require.NoError(t, err)
	require.Equal(t, "offline", node.Status)

	stats, err := repo.Stats(ctx)
	require.NoError(t, err)
	require.Equal(t, repository.GatewayNodeStats{Offline: 1}, stats)

	// 仍标记 online 但已过期的节点单独计数，便于发现残留。
	_, err = backend.DB().ExecContext(ctx,
		`UPDATE gateway_nodes SET status = 'online',
			expires_at = strftime('%Y-%m-%d %H:%M:%f','now','-1 seconds')
		 WHERE gateway_id = ?`, "gw-1")
	require.NoError(t, err)
	stats, err = repo.Stats(ctx)
	require.NoError(t, err)
	require.Equal(t, repository.GatewayNodeStats{Expired: 1}, stats)

	missing, err := repo.Get(ctx, "gw-404")
	require.NoError(t, err)
	require.Nil(t, missing)
}
