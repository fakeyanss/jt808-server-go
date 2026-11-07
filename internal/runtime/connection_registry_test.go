package runtime

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
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
	require.Same(t, session, got, "connection registry must keep the same reference and not return a copy")
	require.Equal(t, 1, registry.Count(ctx))

	registry.Unbind(ctx, session.ID)
	require.Equal(t, 0, registry.Count(ctx))
	_, err = registry.Get(ctx, session.ID)
	require.ErrorIs(t, err, ErrSessionClosed)
}

func TestConnectionRegistryPhoneIndexLifecycle(t *testing.T) {
	registry := NewConnectionRegistry()
	ctx := context.Background()

	s1 := &model.Session{ID: "s1"}
	s2 := &model.Session{ID: "s2"}
	require.NoError(t, registry.Bind(ctx, s1))
	require.NoError(t, registry.Bind(ctx, s2))
	const phone = "18800000001"

	_, err := registry.GetByPhone(ctx, phone)
	require.ErrorIs(t, err, ErrPhoneNotBound, "unregistered phone must report a clear error")

	bound, err := registry.BindPhoneIfFree(ctx, phone, s1.ID)
	require.NoError(t, err)
	require.True(t, bound)
	owner, err := registry.GetByPhone(ctx, phone)
	require.NoError(t, err)
	require.Same(t, s1, owner)

	// 手机号已被占用时，注册阶段不得顶替。
	bound, err = registry.BindPhoneIfFree(ctx, phone, s2.ID)
	require.NoError(t, err)
	require.False(t, bound)
	owner, err = registry.GetByPhone(ctx, phone)
	require.NoError(t, err)
	require.Same(t, s1, owner)

	// 鉴权通过后强制顶替，返回旧会话并切换索引。
	superseded, err := registry.BindPhone(ctx, phone, s2.ID)
	require.NoError(t, err)
	require.Same(t, s1, superseded)
	owner, err = registry.GetByPhone(ctx, phone)
	require.NoError(t, err)
	require.Same(t, s2, owner)

	// 旧会话断开只能清理自身，不得删除顶替者的手机号索引。
	registry.Unbind(ctx, s1.ID)
	owner, err = registry.GetByPhone(ctx, phone)
	require.NoError(t, err)
	require.Same(t, s2, owner)

	// 拥有者断开后手机号索引无残留。
	registry.Unbind(ctx, s2.ID)
	_, err = registry.GetByPhone(ctx, phone)
	require.ErrorIs(t, err, ErrPhoneNotBound)
}

// 已被顶替的会话不得再次绑定：否则"注册期绑定 + 鉴权期再次绑定"的胜出者
// 在并发下会与顶替者互相关闭，形成没有活会话的环。
func TestConnectionRegistrySupersededSessionCannotRebind(t *testing.T) {
	registry := NewConnectionRegistry()
	ctx := context.Background()
	const phone = "18800000004"

	owner := &model.Session{ID: "registration-owner"}
	challenger := &model.Session{ID: "auth-challenger"}
	require.NoError(t, registry.Bind(ctx, owner))
	require.NoError(t, registry.Bind(ctx, challenger))

	// 注册阶段绑定成功。
	bound, err := registry.BindPhoneIfFree(ctx, phone, owner.ID)
	require.NoError(t, err)
	require.True(t, bound)

	// 鉴权阶段另一连接通过校验并顶替。
	superseded, err := registry.BindPhone(ctx, phone, challenger.ID)
	require.NoError(t, err)
	require.Same(t, owner, superseded)
	_, err = registry.Get(ctx, owner.ID)
	require.ErrorIs(t, err, ErrSessionClosed, "superseded session must be removed atomically")

	// 旧会话迟到的再次绑定必须失败，不能再把顶替者摘掉。
	_, err = registry.BindPhone(ctx, phone, owner.ID)
	require.ErrorIs(t, err, ErrSessionClosed)
	got, err := registry.GetByPhone(ctx, phone)
	require.NoError(t, err)
	require.Same(t, challenger, got)
	require.Equal(t, 1, registry.Count(ctx))
}

func TestConnectionRegistryConcurrentBindPhoneConverges(t *testing.T) {
	registry := NewConnectionRegistry()
	ctx := context.Background()
	const count = 64
	const phone = "18800000002"

	sessions := make([]*model.Session, count)
	for i := range sessions {
		sessions[i] = &model.Session{ID: fmt.Sprintf("session-%d", i)}
		require.NoError(t, registry.Bind(ctx, sessions[i]))
	}

	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = registry.BindPhone(ctx, phone, sessions[i].ID)
		}(i)
	}
	wg.Wait()

	owner, err := registry.GetByPhone(ctx, phone)
	require.NoError(t, err)
	// 并发顶替后索引必须收敛到唯一活会话，且该会话仍在注册表中。
	got, err := registry.Get(ctx, owner.ID)
	require.NoError(t, err)
	require.Same(t, owner, got)
	// 被顶替会话在锁内摘除，避免它们互相顶替形成环后没有活会话。
	require.Equal(t, 1, registry.Count(ctx), "concurrent takeover must leave exactly one live session")

	// 断开最终拥有者后索引不再残留。
	registry.Unbind(ctx, owner.ID)
	_, err = registry.GetByPhone(ctx, phone)
	require.ErrorIs(t, err, ErrPhoneNotBound)
}

func TestConnectionRegistryConcurrentBindPhoneIfFreeSingleWinner(t *testing.T) {
	registry := NewConnectionRegistry()
	ctx := context.Background()
	const count = 64
	const phone = "18800000003"

	sessions := make([]*model.Session, count)
	for i := range sessions {
		sessions[i] = &model.Session{ID: fmt.Sprintf("free-session-%d", i)}
		require.NoError(t, registry.Bind(ctx, sessions[i]))
	}

	var winners int32
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			bound, err := registry.BindPhoneIfFree(ctx, phone, sessions[i].ID)
			if err == nil && bound {
				atomic.AddInt32(&winners, 1)
			}
		}(i)
	}
	wg.Wait()

	require.EqualValues(t, 1, atomic.LoadInt32(&winners), "exactly one session may claim a free phone")
	_, err := registry.GetByPhone(ctx, phone)
	require.NoError(t, err)
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
