package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

func TestSessionDirectoryRepositoryLifecycle(t *testing.T) {
	ctx := context.Background()
	dir := NewSessionDirectoryRepository()
	now := time.Unix(1_800_000_000, 0)

	owner := repository.SessionOwner{GatewayID: "gw-1", InstanceID: "inst-a", SessionID: "sess-1"}
	generation, err := dir.Claim(ctx, "13800000001", owner, now)
	require.NoError(t, err)
	require.Equal(t, uint64(1), generation)

	generation, err = dir.Claim(ctx, "13800000001", owner, now)
	require.NoError(t, err)
	require.Equal(t, uint64(1), generation)

	next := repository.SessionOwner{GatewayID: "gw-2", InstanceID: "inst-b", SessionID: "sess-2"}
	generation, err = dir.Claim(ctx, "13800000001", next, now)
	require.NoError(t, err)
	require.Equal(t, uint64(2), generation)

	ok, err := dir.Validate(ctx, "13800000001", owner, 1)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = dir.Validate(ctx, "13800000001", next, 2)
	require.NoError(t, err)
	require.True(t, ok)

	require.NoError(t, dir.Release(ctx, "13800000001", owner, 1))
	record, err := dir.Get(ctx, "13800000001")
	require.NoError(t, err)
	require.NotNil(t, record)

	require.NoError(t, dir.Release(ctx, "13800000001", next, 2))
	record, err = dir.Get(ctx, "13800000001")
	require.NoError(t, err)
	require.Nil(t, record)
}
