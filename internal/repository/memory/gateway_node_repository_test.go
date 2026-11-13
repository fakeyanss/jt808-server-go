package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGatewayNodeRepositoryLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := NewGatewayNodeRepository()
	now := time.Unix(1_800_000_000, 0)

	require.NoError(t, repo.Register(ctx, "gw-1", "inst-a", "http://gw-1:9001", now))

	node, err := repo.Get(ctx, "gw-1")
	require.NoError(t, err)
	require.NotNil(t, node)
	require.Equal(t, "inst-a", node.InstanceID)
	require.Equal(t, "http://gw-1:9001", node.DownlinkURL)
	require.Equal(t, "online", node.Status)
	require.False(t, node.ExpiresAt.IsZero())

	ok, err := repo.Heartbeat(ctx, "gw-1", "inst-a", now)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = repo.Heartbeat(ctx, "gw-1", "inst-stale", now)
	require.NoError(t, err)
	require.False(t, ok)

	// 新实例注册后旧实例心跳必须被围栏。
	require.NoError(t, repo.Register(ctx, "gw-1", "inst-b", "http://gw-1:9001", now))
	ok, err = repo.Heartbeat(ctx, "gw-1", "inst-a", now)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, repo.Deregister(ctx, "gw-1", "inst-b", now))
	node, err = repo.Get(ctx, "gw-1")
	require.NoError(t, err)
	require.Equal(t, "offline", node.Status)

	missing, err := repo.Get(ctx, "gw-404")
	require.NoError(t, err)
	require.Nil(t, missing)
}
