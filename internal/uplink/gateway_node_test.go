package uplink

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/gatewaynode"
	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
)

func newGatewayNodeServer(t *testing.T) (*Server, *memory.GatewayNodeRepository, *httptest.Server) {
	t.Helper()
	nodes := memory.NewGatewayNodeRepository()
	server, err := NewServer(ServerOptions{Token: testControlToken})
	require.NoError(t, err)
	server.SetGatewayNodeRepository(nodes)
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return server, nodes, httpServer
}

func postNode(t *testing.T, url, token string, body any) *http.Response {
	t.Helper()
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func TestGatewayNodeEndpointsLifecycle(t *testing.T) {
	_, nodes, httpServer := newGatewayNodeServer(t)

	register := gatewaynode.RegisterRequest{
		GatewayID:   "gw-1",
		InstanceID:  "inst-a",
		DownlinkURL: "http://gw-1:9001",
	}
	response := postNode(t, httpServer.URL+gatewaynode.PathRegister, testControlToken, register)
	require.Equal(t, http.StatusOK, response.StatusCode)

	heartbeat := gatewaynode.HeartbeatRequest{GatewayID: "gw-1", InstanceID: "inst-a"}
	response = postNode(t, httpServer.URL+gatewaynode.PathHeartbeat, testControlToken, heartbeat)
	require.Equal(t, http.StatusOK, response.StatusCode)

	// 旧实例被新注册围栏。
	register.InstanceID = "inst-b"
	response = postNode(t, httpServer.URL+gatewaynode.PathRegister, testControlToken, register)
	require.Equal(t, http.StatusOK, response.StatusCode)

	response = postNode(t, httpServer.URL+gatewaynode.PathHeartbeat, testControlToken, heartbeat)
	require.Equal(t, http.StatusConflict, response.StatusCode)
	var fenced gatewaynode.StatusResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&fenced))
	require.Equal(t, "instance_stale", fenced.Code)

	current := gatewaynode.HeartbeatRequest{GatewayID: "gw-1", InstanceID: "inst-b"}
	response = postNode(t, httpServer.URL+gatewaynode.PathDeregister, testControlToken, current)
	require.Equal(t, http.StatusOK, response.StatusCode)

	node, err := nodes.Get(context.Background(), "gw-1")
	require.NoError(t, err)
	require.NotNil(t, node)
	require.Equal(t, "offline", node.Status)
}

func TestGatewayNodeEndpointsValidateInput(t *testing.T) {
	_, _, httpServer := newGatewayNodeServer(t)

	response := postNode(t, httpServer.URL+gatewaynode.PathRegister, "", gatewaynode.RegisterRequest{})
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)

	response = postNode(t, httpServer.URL+gatewaynode.PathRegister, testControlToken,
		gatewaynode.RegisterRequest{GatewayID: "gw-1"})
	require.Equal(t, http.StatusBadRequest, response.StatusCode)

	response = postNode(t, httpServer.URL+gatewaynode.PathHeartbeat, testControlToken,
		gatewaynode.HeartbeatRequest{GatewayID: "gw-1"})
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
}

func TestGatewayNodeEndpointsHiddenWithoutRepository(t *testing.T) {
	server, err := NewServer(ServerOptions{Token: testControlToken})
	require.NoError(t, err)
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	response := postNode(t, httpServer.URL+gatewaynode.PathRegister, testControlToken,
		gatewaynode.RegisterRequest{GatewayID: "gw-1", InstanceID: "inst-a", DownlinkURL: "http://gw-1:9001"})
	require.Equal(t, http.StatusNotFound, response.StatusCode)
}
