package adminapi

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
)

type sessionFixture struct {
	server    *Server
	registry  runtime.ConnectionRegistry
	devices   repository.DeviceRepository
	heartbeat runtime.HeartbeatRegistry
	peers     []net.Conn
}

func newSessionFixture(t *testing.T) *sessionFixture {
	t.Helper()
	fixture := &sessionFixture{
		registry:  runtime.NewConnectionRegistry(),
		devices:   repository.NewDeviceRepository(),
		heartbeat: runtime.NewHeartbeatRegistry(),
	}
	fixture.server = NewServer(testOptions("secret", 100, 200), Dependencies{
		Conn:      fixture.registry,
		Heartbeat: fixture.heartbeat,
		Devices:   fixture.devices,
	})
	t.Cleanup(func() {
		for _, peer := range fixture.peers {
			_ = peer.Close()
		}
	})
	return fixture
}

// bindSession 创建一个带真实 net.Conn 的已绑定会话，便于验证踢线会关闭连接。
func (f *sessionFixture) bindSession(t *testing.T, phone, remoteAddr string, connectedAt time.Time) *model.Session {
	t.Helper()
	serverConn, peer := net.Pipe()
	f.peers = append(f.peers, peer)

	session := &model.Session{ID: remoteAddr, Conn: serverConn, ConnectedAt: connectedAt}
	require.NoError(t, f.registry.Bind(context.Background(), session))
	bound, err := f.registry.BindPhoneIfFree(context.Background(), phone, session.ID)
	require.NoError(t, err)
	require.True(t, bound)
	return session
}

func TestListSessionsReturnsBoundSessionsWithRuntimeFields(t *testing.T) {
	fixture := newSessionFixture(t)
	connectedAt := time.Now().Add(-time.Hour).Truncate(time.Second)
	lastCom := time.Now().Add(-time.Minute).Truncate(time.Second)
	fixture.bindSession(t, "18800000001", "10.0.0.1:9000", connectedAt)
	fixture.bindSession(t, "18800000002", "10.0.0.2:9001", connectedAt)
	// 未绑定手机号的连接不属于在线设备，不应出现在列表里。
	unbound := &model.Session{ID: "10.0.0.3:9002", ConnectedAt: connectedAt}
	require.NoError(t, fixture.registry.Bind(context.Background(), unbound))

	require.NoError(t, fixture.devices.Upsert(context.Background(), &model.Device{
		Phone:           "18800000001",
		ProtocolVersion: 1,
		LastestComTime:  lastCom,
	}))
	require.NoError(t, fixture.devices.Upsert(context.Background(), &model.Device{
		Phone:           "18800000002",
		ProtocolVersion: 0,
		LastestComTime:  lastCom,
	}))
	// 心跳时间比库内最后交互时间更新，管理面应取较新值。
	heartbeatAt := lastCom.Add(30 * time.Second)
	fixture.heartbeat.Touch(context.Background(), "18800000001", heartbeatAt)

	recorder := doRequest(t, fixture.server.Handler(), http.MethodGet, "/sessions", "secret")
	require.Equal(t, http.StatusOK, recorder.Code)

	var response sessionListResponse
	require.NoError(t, decodeJSON(recorder, &response))
	require.Equal(t, 2, response.Total)
	require.Equal(t, 1, response.Page)
	require.Equal(t, defaultPageSize, response.PageSize)
	require.Len(t, response.Items, 2)

	first := response.Items[0]
	require.Equal(t, "18800000001", first.Phone)
	require.Equal(t, "10.0.0.1:9000", first.RemoteAddr)
	require.Equal(t, string(model.TCPProto), first.Transport)
	require.Equal(t, uint8(1), first.ProtocolVersion)
	require.Equal(t, connectedAt.Format(time.RFC3339), first.ConnectedAt)
	require.Equal(t, heartbeatAt.Format(time.RFC3339), first.LastComTime)

	second := response.Items[1]
	require.Equal(t, "18800000002", second.Phone)
	require.Equal(t, lastCom.Format(time.RFC3339), second.LastComTime)
}

func TestListSessionsPaginates(t *testing.T) {
	fixture := newSessionFixture(t)
	for _, phone := range []string{"18800000001", "18800000002", "18800000003"} {
		fixture.bindSession(t, phone, phone+":9000", time.Now())
	}

	recorder := doRequest(t, fixture.server.Handler(), http.MethodGet, "/sessions?page=2&pageSize=1", "secret")
	require.Equal(t, http.StatusOK, recorder.Code)

	var response sessionListResponse
	require.NoError(t, decodeJSON(recorder, &response))
	require.Equal(t, 3, response.Total)
	require.Equal(t, 2, response.Page)
	require.Equal(t, 1, response.PageSize)
	require.Len(t, response.Items, 1)
	require.Equal(t, "18800000002", response.Items[0].Phone)
}

func TestListSessionsRejectsInvalidPagination(t *testing.T) {
	fixture := newSessionFixture(t)
	for _, path := range []string{"/sessions?page=0", "/sessions?pageSize=0", "/sessions?pageSize=101", "/sessions?page=abc"} {
		recorder := doRequest(t, fixture.server.Handler(), http.MethodGet, path, "secret")
		require.Equal(t, http.StatusBadRequest, recorder.Code, path)
		require.Equal(t, codeBadRequest, decodeError(t, recorder).Code)
	}
}

func TestGetSessionDetailAndNotFound(t *testing.T) {
	fixture := newSessionFixture(t)
	fixture.bindSession(t, "18800000001", "10.0.0.1:9000", time.Now())

	recorder := doRequest(t, fixture.server.Handler(), http.MethodGet, "/sessions/18800000001", "secret")
	require.Equal(t, http.StatusOK, recorder.Code)
	var view sessionView
	require.NoError(t, decodeJSON(recorder, &view))
	require.Equal(t, "18800000001", view.Phone)

	recorder = doRequest(t, fixture.server.Handler(), http.MethodGet, "/sessions/18800000009", "secret")
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Equal(t, codeNotFound, decodeError(t, recorder).Code)
}

func TestKickSessionDisconnectsAndCleansIndex(t *testing.T) {
	fixture := newSessionFixture(t)
	session := fixture.bindSession(t, "18800000001", "10.0.0.1:9000", time.Now())

	recorder := doRequest(t, fixture.server.Handler(), http.MethodDelete, "/sessions/18800000001", "secret")
	require.Equal(t, http.StatusOK, recorder.Code)

	var response struct {
		Phone        string `json:"phone"`
		SessionID    string `json:"sessionId"`
		Disconnected bool   `json:"disconnected"`
	}
	require.NoError(t, decodeJSON(recorder, &response))
	require.Equal(t, "18800000001", response.Phone)
	require.Equal(t, session.ID, response.SessionID)
	require.True(t, response.Disconnected)

	_, err := fixture.registry.GetByPhone(context.Background(), "18800000001")
	require.ErrorIs(t, err, runtime.ErrPhoneNotBound, "kick must remove the phone index")
	_, err = fixture.registry.Get(context.Background(), session.ID)
	require.ErrorIs(t, err, runtime.ErrSessionClosed)

	recorder = doRequest(t, fixture.server.Handler(), http.MethodDelete, "/sessions/18800000001", "secret")
	require.Equal(t, http.StatusNotFound, recorder.Code, "repeated kick must be distinguishable from success")
	require.Equal(t, codeNotFound, decodeError(t, recorder).Code)
}

func TestSessionRoutesRequireAuth(t *testing.T) {
	fixture := newSessionFixture(t)
	recorder := doRequest(t, fixture.server.Handler(), http.MethodGet, "/sessions", "")
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Equal(t, codeUnauthorized, decodeError(t, recorder).Code)
}

func decodeJSON(recorder *httptest.ResponseRecorder, target any) error {
	return json.Unmarshal(recorder.Body.Bytes(), target)
}
