package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

type endSessionHooks struct {
	action.Noop
	disconnects []action.DisconnectEvent
}

func (h *endSessionHooks) OnDisconnect(_ context.Context, event action.DisconnectEvent) {
	h.disconnects = append(h.disconnects, event)
}

// 注销与连接关闭都必须幂等结束会话；cluster 网关不重复派发 action 钩子。
func TestEndSessionIsIdempotentAndRespectsSkipActionHooks(t *testing.T) {
	var released int
	deps := protocol.ProcessorDeps{
		SkipActionHooks: true,
		OnSessionClosed: func(string, string, string, uint64) { released++ },
	}
	hooks := &endSessionHooks{}
	session := &model.Session{ID: "sess-cluster", Transport: model.TCPProto}

	endSession(session, hooks, deps, "logout")
	endSession(session, hooks, deps, "transport_closed")

	require.Equal(t, 1, released)
	require.Empty(t, hooks.disconnects)
}

// standalone 保持本地派发，并带上结束原因。
func TestEndSessionDispatchesReasonWhenHooksLocal(t *testing.T) {
	hooks := &endSessionHooks{}
	session := &model.Session{ID: "sess-standalone", Transport: model.TCPProto}

	endSession(session, hooks, protocol.ProcessorDeps{}, "logout")

	require.Len(t, hooks.disconnects, 1)
	require.Equal(t, "sess-standalone", hooks.disconnects[0].EventID)
	require.Equal(t, "logout", hooks.disconnects[0].Reason)
}
