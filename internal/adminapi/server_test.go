package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
)

func testOptions(token string, rate float64, burst int) config.AdminOptions {
	return config.AdminOptions{
		Addr:          "127.0.0.1:0",
		Token:         token,
		RatePerSecond: rate,
		Burst:         burst,
		KeyBy:         config.AdminRateKeyBySource,
	}
}

func doRequest(t *testing.T, handler http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func decodeError(t *testing.T, recorder *httptest.ResponseRecorder) errorBody {
	t.Helper()
	require.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
	var body errorBody
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	return body
}

func TestAdminHealthzIsPublic(t *testing.T) {
	serv := NewServer(testOptions("secret", 10, 20), Dependencies{})
	recorder := doRequest(t, serv.Handler(), http.MethodGet, "/healthz", "")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"status":"ok"}`, recorder.Body.String())
}

func TestAdminAuthRejectsMissingAndInvalidToken(t *testing.T) {
	serv := NewServer(testOptions("secret", 10, 20), Dependencies{})

	recorder := doRequest(t, serv.Handler(), http.MethodGet, "/ping", "")
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Equal(t, codeUnauthorized, decodeError(t, recorder).Code)

	recorder = doRequest(t, serv.Handler(), http.MethodGet, "/ping", "wrong")
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Equal(t, codeUnauthorized, decodeError(t, recorder).Code)

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Authorization", "Basic secret")
	recorder = httptest.NewRecorder()
	serv.Handler().ServeHTTP(recorder, req)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Equal(t, codeUnauthorized, decodeError(t, recorder).Code)
}

func TestAdminAuthAcceptsValidToken(t *testing.T) {
	serv := NewServer(testOptions("secret", 10, 20), Dependencies{})
	recorder := doRequest(t, serv.Handler(), http.MethodGet, "/ping", "secret")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"status":"ok"}`, recorder.Body.String())
}

func TestAdminRateLimitBySource(t *testing.T) {
	serv := NewServer(testOptions("secret", 1, 1), Dependencies{})

	recorder := doRequest(t, serv.Handler(), http.MethodGet, "/ping", "secret")
	require.Equal(t, http.StatusOK, recorder.Code)

	recorder = doRequest(t, serv.Handler(), http.MethodGet, "/ping", "secret")
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.NotEmpty(t, recorder.Header().Get("Retry-After"))
	require.Equal(t, codeRateLimited, decodeError(t, recorder).Code)
}

func TestAdminUnknownRouteUsesJSONError(t *testing.T) {
	serv := NewServer(testOptions("secret", 10, 20), Dependencies{})
	recorder := doRequest(t, serv.Handler(), http.MethodGet, "/missing", "secret")
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Equal(t, codeNotFound, decodeError(t, recorder).Code)
}

func TestAdminReadyzReflectsFailedChecks(t *testing.T) {
	serv := NewServer(testOptions("secret", 10, 20), Dependencies{
		Probes: &observability.Provider{Checks: []observability.Check{
			{Name: "database", Fn: func(context.Context) error { return errors.New("database is down") }},
		}},
	})

	recorder := doRequest(t, serv.Handler(), http.MethodGet, "/readyz", "")
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"status":"not ready"`)
	require.Contains(t, recorder.Body.String(), `"name":"database"`)
	require.Contains(t, recorder.Body.String(), "database is down")
}

func TestAdminReadyzDefaultsToReady(t *testing.T) {
	serv := NewServer(testOptions("secret", 10, 20), Dependencies{})
	recorder := doRequest(t, serv.Handler(), http.MethodGet, "/readyz", "")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"status":"ready","checks":[]}`, recorder.Body.String())
}

func TestAdminMetricsRequiresToken(t *testing.T) {
	serv := NewServer(testOptions("secret", 10, 20), Dependencies{})

	recorder := doRequest(t, serv.Handler(), http.MethodGet, "/metrics", "")
	require.Equal(t, http.StatusUnauthorized, recorder.Code)

	recorder = doRequest(t, serv.Handler(), http.MethodGet, "/metrics", "secret")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "text/plain; version=0.0.4; charset=utf-8", recorder.Header().Get("Content-Type"))
	require.Contains(t, recorder.Body.String(), "jt808_sessions_connected 0")
	require.Contains(t, recorder.Body.String(), "jt808_attachments_completed_total 0")
}
