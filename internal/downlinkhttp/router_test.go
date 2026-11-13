package downlinkhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// stubSessionDirectory 只关心 Route；其余方法在 Router 用例中不会被调用。
type stubSessionDirectory struct {
	route      func(ctx context.Context, phone, sessionID string) (string, bool, error)
	routeCalls int32
}

func (s *stubSessionDirectory) Claim(context.Context, string, repository.SessionOwner) (uint64, error) {
	return 0, nil
}

func (s *stubSessionDirectory) Release(context.Context, string, repository.SessionOwner, uint64) error {
	return nil
}

func (s *stubSessionDirectory) Validate(context.Context, string, repository.SessionOwner, uint64) (bool, error) {
	return false, nil
}

func (s *stubSessionDirectory) Get(context.Context, string) (*repository.SessionRecord, error) {
	return nil, nil
}

func (s *stubSessionDirectory) Route(ctx context.Context, phone, sessionID string) (string, bool, error) {
	atomic.AddInt32(&s.routeCalls, 1)
	return s.route(ctx, phone, sessionID)
}

// 命中目录时把请求原样转发到归属节点。
func TestRouterSendsToDirectoryNode(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, PathDownlink, r.URL.Path)
		require.Equal(t, "Bearer "+testToken, r.Header.Get("Authorization"))
		var body downlinkRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, testPhone, body.Phone)
		require.Equal(t, "sess-1", body.SessionID)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(downlinkResponse{
			DownlinkID: "dl-1",
			Serial:     body.Serial,
			Status:     contracts.DownlinkStatusAccepted,
		}))
	}))
	defer gateway.Close()

	dir := &stubSessionDirectory{route: func(_ context.Context, phone, sessionID string) (string, bool, error) {
		if phone != testPhone || sessionID != "sess-1" {
			return "", false, nil
		}
		return gateway.URL, true, nil
	}}
	metrics := observability.NewMetrics()
	router, err := NewRouter(RouterOptions{Directory: dir, Token: testToken, Timeout: time.Second, Metrics: metrics})
	require.NoError(t, err)

	msg, _ := testMessage(t, testPhone)
	receipt, err := router.Send(context.Background(), contracts.DownlinkRequest{
		Phone:     testPhone,
		SessionID: "sess-1",
		Serial:    7,
		Message:   msg,
	})
	require.NoError(t, err)
	require.Equal(t, contracts.DownlinkStatusAccepted, receipt.Status)
	require.Equal(t, "dl-1", receipt.ID)
	require.Equal(t, int64(1), metrics.ClusterEventSnapshot().DownlinkDirectoryHits)
	require.Zero(t, metrics.ClusterEventSnapshot().DownlinkOffline)
}

// 没有目录或节点不可用时返回 offline，且不得访问任何节点。
func TestRouterReturnsOfflineWithoutSending(t *testing.T) {
	var hits int32
	gateway := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer gateway.Close()

	dir := &stubSessionDirectory{route: func(context.Context, string, string) (string, bool, error) {
		return "", false, nil
	}}
	metrics := observability.NewMetrics()
	router, err := NewRouter(RouterOptions{Directory: dir, Token: testToken, Timeout: time.Second, Metrics: metrics})
	require.NoError(t, err)

	msg, _ := testMessage(t, testPhone)
	receipt, err := router.Send(context.Background(), contracts.DownlinkRequest{
		Phone:     testPhone,
		SessionID: "sess-stale",
		Serial:    7,
		Message:   msg,
	})
	require.NoError(t, err)
	require.Equal(t, contracts.DownlinkStatusOffline, receipt.Status)
	require.Zero(t, atomic.LoadInt32(&hits))
	require.Equal(t, int64(1), metrics.ClusterEventSnapshot().DownlinkOffline)
	require.Zero(t, metrics.ClusterEventSnapshot().DownlinkDirectoryHits)
}

// 目录查询失败属于可重试的传输错误，不能伪装成 offline。
func TestRouterDirectoryFailureIsTransient(t *testing.T) {
	dir := &stubSessionDirectory{route: func(context.Context, string, string) (string, bool, error) {
		return "", false, errors.New("database unavailable")
	}}
	router, err := NewRouter(RouterOptions{Directory: dir, Token: testToken, Timeout: time.Second})
	require.NoError(t, err)

	msg, _ := testMessage(t, testPhone)
	_, err = router.Send(context.Background(), contracts.DownlinkRequest{
		Phone:   testPhone,
		Message: msg,
	})
	require.Error(t, err)
	require.Equal(t, contracts.DownlinkErrorTransient, contracts.ClassifyDownlinkError(err).Kind)
}

// 没有 phone 时无从查目录，直接按 offline 处理。
func TestRouterEmptyPhoneIsOffline(t *testing.T) {
	dir := &stubSessionDirectory{route: func(context.Context, string, string) (string, bool, error) {
		return "", true, nil
	}}
	router, err := NewRouter(RouterOptions{Directory: dir, Token: testToken, Timeout: time.Second})
	require.NoError(t, err)

	msg, _ := testMessage(t, testPhone)
	receipt, err := router.Send(context.Background(), contracts.DownlinkRequest{Message: msg})
	require.NoError(t, err)
	require.Equal(t, contracts.DownlinkStatusOffline, receipt.Status)
	require.Zero(t, atomic.LoadInt32(&dir.routeCalls))
}
