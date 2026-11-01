package storage

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

func TestConnectionRegistryBindGetUnbind(t *testing.T) {
	registry := NewConnectionRegistry()
	ctx := context.Background()

	_, err := registry.Get(ctx, "missing")
	require.ErrorIs(t, err, ErrSessionClosed)

	session := &model.Session{ID: "tcp/10.0.0.1:9000"}
	require.NoError(t, registry.Bind(ctx, session))

	got, err := registry.Get(ctx, session.ID)
	require.NoError(t, err)
	require.Same(t, session, got, "连接现场按引用保存，不返回副本")
	require.Equal(t, 1, registry.Count(ctx))

	registry.Unbind(ctx, session.ID)
	require.Equal(t, 0, registry.Count(ctx))
	_, err = registry.Get(ctx, session.ID)
	require.ErrorIs(t, err, ErrSessionClosed)
}

func TestConnectionRegistryConcurrentAccess(t *testing.T) {
	registry := NewConnectionRegistry()
	ctx := context.Background()
	const count = 128

	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			session := &model.Session{ID: fmt.Sprintf("s-%d", i)}
			_ = registry.Bind(ctx, session)
			_, _ = registry.Get(ctx, session.ID)
			_ = registry.Count(ctx)
			registry.Unbind(ctx, session.ID)
		}(i)
	}
	wg.Wait()

	require.Equal(t, 0, registry.Count(ctx))
}
