package controlhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/internalapi"
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

func postGenerationEnvelope(t *testing.T, url, token string, envelope internalapi.UplinkEnvelope) (*http.Response, map[string]any) {
	t.Helper()
	payload, err := json.Marshal(envelope)
	require.NoError(t, err)
	request, err := http.NewRequest(http.MethodPost, url+internalapi.PathUplink, bytes.NewReader(payload))
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
	now := time.Now()
	stale := repository.SessionOwner{GatewayID: "gw-1", InstanceID: "inst-a", SessionID: "sess-0"}
	_, err := sessions.Claim(context.Background(), "13800000001", stale, now)
	require.NoError(t, err)
	owner := repository.SessionOwner{GatewayID: "gw-1", InstanceID: "inst-a", SessionID: "sess-1"}
	generation, err := sessions.Claim(context.Background(), "13800000001", owner, now)
	require.NoError(t, err)
	require.Equal(t, uint64(2), generation)
	ok, err := sessions.Validate(context.Background(), "13800000001", owner, generation)
	require.NoError(t, err)
	require.True(t, ok)

	envelope := internalapi.UplinkEnvelope{
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

	envelope := internalapi.UplinkEnvelope{
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

func TestUplinkServerSessionClosedIsIdempotent(t *testing.T) {
	sessions := memory.NewSessionDirectoryRepository()
	type closed struct{ phone, sessionID, transport, reason string }
	var got []closed
	server, err := NewServer(ServerOptions{
		Token: testControlToken,
		OnSessionClosed: func(_ context.Context, phone, sessionID, transport, reason string) {
			got = append(got, closed{phone: phone, sessionID: sessionID, transport: transport, reason: reason})
		},
	})
	require.NoError(t, err)
	server.SetSessionDirectoryRepository(sessions)
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	owner := repository.SessionOwner{GatewayID: "gw-1", InstanceID: "inst-a", SessionID: "sess-1"}
	generation, err := sessions.Claim(context.Background(), "13800000003", owner, time.Now())
	require.NoError(t, err)

	envelope := internalapi.UplinkEnvelope{
		EventID:     owner.SessionID,
		Event:       "session.closed",
		Phone:       "13800000003",
		GatewayID:   owner.GatewayID,
		InstanceID:  owner.InstanceID,
		SessionID:   owner.SessionID,
		Generation:  generation,
		Transport:   "TCP",
		Reason:      "logout",
		Reliability: string(contracts.ReliabilityAtLeastOnce),
	}
	response, body := postGenerationEnvelope(t, httpServer.URL, testControlToken, envelope)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "closed", body["outcome"])

	record, err := sessions.Get(context.Background(), "13800000003")
	require.NoError(t, err)
	require.Nil(t, record)
	require.Equal(t, []closed{{phone: "13800000003", sessionID: "sess-1", transport: "TCP", reason: "logout"}}, got)

	// 同一 eventId 重投：去重短路，不再释放也不再回调。
	response, body = postGenerationEnvelope(t, httpServer.URL, testControlToken, envelope)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "duplicate", body["outcome"])

	// 同一会话换 eventId 重投：释放条件不再匹配，只回 already_closed。
	envelope.EventID = "sess-1-retry"
	response, body = postGenerationEnvelope(t, httpServer.URL, testControlToken, envelope)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "already_closed", body["outcome"])
	require.Len(t, got, 1)
}

// 归属条件不匹配（旧 generation）时不得释放别人的新会话。
func TestUplinkServerSessionClosedFencesStaleOwner(t *testing.T) {
	sessions := memory.NewSessionDirectoryRepository()
	closedCalls := 0
	server, err := NewServer(ServerOptions{
		Token: testControlToken,
		OnSessionClosed: func(context.Context, string, string, string, string) {
			closedCalls++
		},
	})
	require.NoError(t, err)
	server.SetSessionDirectoryRepository(sessions)
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	now := time.Now()
	stale := repository.SessionOwner{GatewayID: "gw-1", InstanceID: "inst-a", SessionID: "sess-1"}
	staleGeneration, err := sessions.Claim(context.Background(), "13800000004", stale, now)
	require.NoError(t, err)
	current := repository.SessionOwner{GatewayID: "gw-1", InstanceID: "inst-b", SessionID: "sess-2"}
	_, err = sessions.Claim(context.Background(), "13800000004", current, now)
	require.NoError(t, err)

	envelope := internalapi.UplinkEnvelope{
		EventID:     stale.SessionID,
		Event:       "session.closed",
		Phone:       "13800000004",
		GatewayID:   stale.GatewayID,
		InstanceID:  stale.InstanceID,
		SessionID:   stale.SessionID,
		Generation:  staleGeneration,
		Transport:   "TCP",
		Reason:      "transport_closed",
		Reliability: string(contracts.ReliabilityAtLeastOnce),
	}
	response, body := postGenerationEnvelope(t, httpServer.URL, testControlToken, envelope)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "already_closed", body["outcome"])
	require.Zero(t, closedCalls)

	record, err := sessions.Get(context.Background(), "13800000004")
	require.NoError(t, err)
	require.Equal(t, current, record.SessionOwner)
}
