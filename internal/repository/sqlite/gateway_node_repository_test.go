package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

	// 新实例注册后，旧实例心跳必须被围栏。
	require.NoError(t, repo.Register(ctx, "gw-1", "inst-b", "http://gw-1:9001"))
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

	missing, err := repo.Get(ctx, "gw-404")
	require.NoError(t, err)
	require.Nil(t, missing)
}
