package uplink

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/gatewaynode"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/repository/memory"
)

type resultConsumer struct {
	result contracts.UplinkResult
	calls  int
}

func (c *resultConsumer) ConsumeEvent(context.Context, contracts.UplinkEvent) (contracts.UplinkResult, error) {
	c.calls++
	return c.result, nil
}

func postGenerationEnvelope(t *testing.T, url, token string, envelope HTTPEnvelope) (*http.Response, map[string]any) {
	t.Helper()
	payload, err := json.Marshal(envelope)
	require.NoError(t, err)
	request, err := http.NewRequest(http.MethodPost, url+PathUplink, bytes.NewReader(payload))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	var body map[string]any
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	return response, body
}

func TestUplinkServerFencesStaleGeneration(t *testing.T) {
	consumer := &resultConsumer{result: contracts.UplinkResult{Accepted: true}}
	server, httpServer := newTestServer(t, ServerOptions{})
	server.RegisterConsumer(0x0200, consumer)

	sessions := memory.NewSessionDirectoryRepository()
	server.SetSessionDirectoryRepository(sessions)
	stale := repository.SessionOwner{GatewayID: "gw-1", InstanceID: "inst-a", SessionID: "sess-0"}
	_, err := sessions.Claim(context.Background(), "13800000001", stale)
	require.NoError(t, err)
	owner := repository.SessionOwner{GatewayID: "gw-1", InstanceID: "inst-a", SessionID: "sess-1"}
	generation, err := sessions.Claim(context.Background(), "13800000001", owner)
	require.NoError(t, err)
	require.Equal(t, uint64(2), generation)
	ok, err := sessions.Validate(context.Background(), "13800000001", owner, generation)
	require.NoError(t, err)
	require.True(t, ok)

	envelope := HTTPEnvelope{
		EventID:     "evt-current",
		Phone:       "13800000001",
		MsgID:       0x0200,
		GatewayID:   owner.GatewayID,
		InstanceID:  owner.InstanceID,
		SessionID:   owner.SessionID,
		Generation:  generation,
		Reliability: string(contracts.ReliabilityBestEffort),
	}
	response, body := postGenerationEnvelope(t, httpServer.URL, testControlToken, envelope)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "accepted", body["outcome"])
	require.Equal(t, 1, consumer.calls)

	// 旧 generation 直接 fenced，不进入消费者。
	envelope.EventID = "evt-stale"
	envelope.Generation = generation - 1
	response, body = postGenerationEnvelope(t, httpServer.URL, testControlToken, envelope)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "fenced", body["outcome"])
	require.Equal(t, true, body["fenced"])
	require.Equal(t, 1, consumer.calls)
}

func TestUplinkServerAuthClaimBypassesDedup(t *testing.T) {
	consumer := &resultConsumer{result: contracts.UplinkResult{Accepted: true, Generation: 4}}
	server, httpServer := newTestServer(t, ServerOptions{})
	server.RegisterConsumer(0x0102, consumer)
	server.SetSessionDirectoryRepository(memory.NewSessionDirectoryRepository())

	envelope := HTTPEnvelope{
		EventID:     "evt-auth",
		Phone:       "13800000002",
		MsgID:       0x0102,
		GatewayID:   "gw-1",
		InstanceID:  "inst-a",
		SessionID:   "sess-1",
		Reliability: string(contracts.ReliabilityConfirmBeforeAck),
	}
	response, body := postGenerationEnvelope(t, httpServer.URL, testControlToken, envelope)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, float64(4), body["generation"])

	// 同一 eventId 重试仍要重新 claim 并返回 generation。
	response, body = postGenerationEnvelope(t, httpServer.URL, testControlToken, envelope)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, float64(4), body["generation"])
	require.Equal(t, 2, consumer.calls)
}

func TestUplinkServerSessionRelease(t *testing.T) {
	sessions := memory.NewSessionDirectoryRepository()
	server, err := NewServer(ServerOptions{Token: testControlToken})
	require.NoError(t, err)
	server.SetSessionDirectoryRepository(sessions)
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	owner := repository.SessionOwner{GatewayID: "gw-1", InstanceID: "inst-a", SessionID: "sess-1"}
	generation, err := sessions.Claim(context.Background(), "13800000003", owner)
	require.NoError(t, err)

	release := gatewaynode.ReleaseRequest{
		GatewayID:  owner.GatewayID,
		InstanceID: owner.InstanceID,
		Phone:      "13800000003",
		SessionID:  owner.SessionID,
		Generation: generation,
	}
	payload, err := json.Marshal(release)
	require.NoError(t, err)
	request, err := http.NewRequest(http.MethodPost, httpServer.URL+gatewaynode.PathRelease, bytes.NewReader(payload))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+testControlToken)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)

	record, err := sessions.Get(context.Background(), "13800000003")
	require.NoError(t, err)
	require.Nil(t, record)
}
