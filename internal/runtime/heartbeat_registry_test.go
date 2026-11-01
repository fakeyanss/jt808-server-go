package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHeartbeatRegistryTouchLastRemove(t *testing.T) {
	ctx := context.Background()
	registry := NewHeartbeatRegistry()
	at := time.Now().Add(-time.Second)

	_, ok := registry.Last(ctx, "188765400010")
	require.False(t, ok)

	registry.Touch(ctx, "188765400010", at)
	got, ok := registry.Last(ctx, "188765400010")
	require.True(t, ok)
	require.Equal(t, at, got)

	// 更早的时间不应覆盖较新记录。
	registry.Touch(ctx, "188765400010", at.Add(-time.Second))
	got, _ = registry.Last(ctx, "188765400010")
	require.Equal(t, at, got)

	registry.Remove(ctx, "188765400010")
	_, ok = registry.Last(ctx, "188765400010")
	require.False(t, ok)
}
