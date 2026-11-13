package uplink

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/medianode"
	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
)

func newMediaNodeServer(t *testing.T) (*memory.MediaNodeRepository, *httptest.Server) {
	t.Helper()
	nodes := memory.NewMediaNodeRepository()
	server, err := NewServer(ServerOptions{Token: testControlToken})
	require.NoError(t, err)
	server.SetMediaNodeRepository(nodes)
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return nodes, httpServer
}

func TestMediaNodeEndpointsLifecycle(t *testing.T) {
	nodes, httpServer := newMediaNodeServer(t)

	register := medianode.RegisterRequest{
		NodeID:           "media-1",
		InstanceID:       "inst-a",
		InternalURL:      "http://media-1:7301",
		PushIP:           "10.0.0.8",
		PushTCPPort:      7300,
		PushUDPPort:      7300,
		PlaybackBaseURL:  "http://media-1:7301/live",
		EndpointCapacity: 16,
		MaxBitrateKbps:   20000,
	}
	response := postNode(t, httpServer.URL+medianode.PathRegister, testControlToken, register)
	require.Equal(t, http.StatusOK, response.StatusCode)

	// CPU 采集不到时缺省上报，心跳必须成功。
	heartbeat := medianode.HeartbeatRequest{
		NodeID:     "media-1",
		InstanceID: "inst-a",
		Load: medianode.Load{
			ActiveChannels:     3,
			ActiveTCPConns:     2,
			InboundBitrateKbps: 8000,
		},
	}
	response = postNode(t, httpServer.URL+medianode.PathHeartbeat, testControlToken, heartbeat)
	require.Equal(t, http.StatusOK, response.StatusCode)

	node, err := nodes.Get(context.Background(), "media-1")
	require.NoError(t, err)
	require.NotNil(t, node)
	require.Equal(t, int64(3), node.ActiveChannels)
	require.Equal(t, int64(8000), node.InboundBitrateKbps)
	require.Nil(t, node.CPUPercent)
	require.Equal(t, "10.0.0.8", node.PushIP)
	require.Equal(t, uint16(7300), node.PushTCPPort)
	require.Equal(t, int64(16), node.EndpointCapacity)

	// 新实例注册顶替旧实例，旧心跳被围栏。
	register.InstanceID = "inst-b"
	response = postNode(t, httpServer.URL+medianode.PathRegister, testControlToken, register)
	require.Equal(t, http.StatusOK, response.StatusCode)

	response = postNode(t, httpServer.URL+medianode.PathHeartbeat, testControlToken, heartbeat)
	require.Equal(t, http.StatusConflict, response.StatusCode)
	var fenced medianode.StatusResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&fenced))
	require.Equal(t, "instance_stale", fenced.Code)

	current := medianode.HeartbeatRequest{NodeID: "media-1", InstanceID: "inst-b"}
	response = postNode(t, httpServer.URL+medianode.PathDeregister, testControlToken, current)
	require.Equal(t, http.StatusOK, response.StatusCode)

	node, err = nodes.Get(context.Background(), "media-1")
	require.NoError(t, err)
	require.Equal(t, "offline", node.Status)
}

func TestMediaNodeEndpointsValidateInput(t *testing.T) {
	_, httpServer := newMediaNodeServer(t)

	response := postNode(t, httpServer.URL+medianode.PathRegister, "", medianode.RegisterRequest{})
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)

	response = postNode(t, httpServer.URL+medianode.PathRegister, testControlToken,
		medianode.RegisterRequest{NodeID: "media-1", InstanceID: "inst-a"})
	require.Equal(t, http.StatusBadRequest, response.StatusCode)

	response = postNode(t, httpServer.URL+medianode.PathHeartbeat, testControlToken,
		medianode.HeartbeatRequest{NodeID: "media-1"})
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
}
