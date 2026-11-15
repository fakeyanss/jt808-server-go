package server

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

type endSessionHooks struct {
	action.Noop
	disconnects []action.DisconnectEvent
}

func (h *endSessionHooks) OnDisconnect(_ context.Context, event action.DisconnectEvent) {
	h.disconnects = append(h.disconnects, event)
}

// recordingSink 记录会话结束事件，模拟 cluster 网关的上行出口。
type recordingSink struct {
	mu     sync.Mutex
	events []contracts.UplinkEvent
}

func (s *recordingSink) Deliver(_ context.Context, event contracts.UplinkEvent) (contracts.UplinkResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return contracts.UplinkResult{Accepted: true}, nil
}

func (s *recordingSink) recorded() []contracts.UplinkEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]contracts.UplinkEvent(nil), s.events...)
}

// 会话结束必须以 at_least_once 事件投递，并带上归属与结束原因。
func TestEndSessionDeliversSessionClosedEvent(t *testing.T) {
	sink := &recordingSink{}
	deps := protocol.ProcessorDeps{
		UplinkSink: sink,
		GatewayID:  "gw-1",
		InstanceID: "inst-a",
	}
	session := &model.Session{ID: "sess-1", Transport: model.TCPProto}
	session.SetPhone("13800000001")
	session.SetGeneration(7)

	endSession(session, action.Noop{}, deps, "logout")

	require.Len(t, sink.recorded(), 1)
	event := sink.recorded()[0]
	require.Equal(t, "session.closed", event.Event)
	require.Equal(t, session.ID, event.EventID)
	require.Equal(t, "13800000001", event.Phone)
	require.Equal(t, session.ID, event.SessionID)
	require.Equal(t, uint64(7), event.Generation)
	require.Equal(t, "gw-1", event.GatewayID)
	require.Equal(t, "inst-a", event.InstanceID)
	require.Equal(t, string(model.TCPProto), event.Transport)
	require.Equal(t, "logout", event.Reason)
	require.Equal(t, contracts.ReliabilityAtLeastOnce, event.Reliability)
}

// 结束会话是幂等的：重复结束不会重复投递或重复派发本地钩子。
func TestEndSessionIsIdempotent(t *testing.T) {
	sink := &recordingSink{}
	hooks := &endSessionHooks{}
	deps := protocol.ProcessorDeps{UplinkSink: sink}
	session := &model.Session{ID: "sess-2", Transport: model.TCPProto}
	session.SetPhone("13800000002")
	session.SetGeneration(1)

	endSession(session, hooks, deps, "logout")
	endSession(session, hooks, deps, "transport_closed")

	require.Len(t, sink.recorded(), 1)
	require.Empty(t, hooks.disconnects)
}

// 未鉴权连接没有目录归属，不应产生 session.closed 事件。
func TestEndSessionSkipsUplinkWithoutOwnership(t *testing.T) {
	sink := &recordingSink{}
	session := &model.Session{ID: "sess-anonymous", Transport: model.TCPProto}

	endSession(session, action.Noop{}, protocol.ProcessorDeps{
		UplinkSink:      sink,
		SkipActionHooks: true,
	}, "transport_closed")

	require.Empty(t, sink.recorded())
}

// standalone 没有上行出口时保持本地派发，并带上结束原因。
func TestEndSessionDispatchesLocalHookWithoutSink(t *testing.T) {
	hooks := &endSessionHooks{}
	session := &model.Session{ID: "sess-standalone", Transport: model.TCPProto}

	endSession(session, hooks, protocol.ProcessorDeps{}, "logout")

	require.Len(t, hooks.disconnects, 1)
	require.Equal(t, "sess-standalone", hooks.disconnects[0].EventID)
	require.Equal(t, "logout", hooks.disconnects[0].Reason)
}

// 本地钩子被显式关闭时（cluster 网关），没有出口也不得重复派发。
func TestEndSessionRespectsSkipActionHooks(t *testing.T) {
	hooks := &endSessionHooks{}
	session := &model.Session{ID: "sess-skip", Transport: model.TCPProto}

	endSession(session, hooks, protocol.ProcessorDeps{SkipActionHooks: true}, "transport_closed")

	require.Empty(t, hooks.disconnects)
}

type sessionClosedAuthProvider struct{}

func (sessionClosedAuthProvider) Device(_ context.Context, phone string) (*model.Device, error) {
	if phone != "13800000009" {
		return nil, repository.ErrDeviceNotFound
	}
	return &model.Device{Phone: phone}, nil
}

func (sessionClosedAuthProvider) AuthCode(context.Context, string) (string, error) {
	return "auth-code", nil
}

// 0x0003 注销与传输层断连必须落到不同的 reason。
func TestServeSessionCloseReason(t *testing.T) {
	const phone = "13800000009"
	codec := protocol.NewJT808PacketCodec()

	tests := []struct {
		name  string
		drive func(t *testing.T, peer net.Conn)
		want  string
	}{
		{
			name: "0x0003 logout",
			drive: func(t *testing.T, peer net.Conn) {
				packet, err := codec.Encode(&model.Msg0003{Header: uplinkHeader(0x0003, phone, 1)})
				require.NoError(t, err)
				_, err = peer.Write(packet)
				require.NoError(t, err)
			},
			want: "logout",
		},
		{
			name: "transport closed",
			drive: func(t *testing.T, peer net.Conn) {
				require.NoError(t, peer.Close())
			},
			want: "transport_closed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			serverConn, peerConn := net.Pipe()
			session := &model.Session{ID: "sess-" + tc.want, Transport: model.TCPProto, Conn: serverConn}
			session.SetPhone(phone)
			session.SetGeneration(1)

			// 对端持续读取，避免服务端写应答时阻塞在无缓冲管道上。
			go func() {
				_, _ = io.Copy(io.Discard, peerConn)
				_ = peerConn.Close()
			}()

			done := make(chan struct{})
			go func() {
				defer close(done)
				serveSession(session, action.Noop{}, protocol.ProcessorDeps{
					UplinkSink:   sink,
					AuthProvider: sessionClosedAuthProvider{},
				})
			}()

			tc.drive(t, peerConn)
			require.Eventually(t, func() bool {
				return len(sink.recorded()) == 1
			}, 3*time.Second, 10*time.Millisecond)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("serveSession did not exit after session close")
			}

			event := sink.recorded()[0]
			require.Equal(t, "session.closed", event.Event)
			require.Equal(t, tc.want, event.Reason)
			require.Equal(t, contracts.ReliabilityAtLeastOnce, event.Reliability)
		})
	}
}
