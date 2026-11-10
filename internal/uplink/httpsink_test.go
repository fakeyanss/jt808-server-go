package uplink

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

func testEvent(reliability contracts.Reliability) contracts.UplinkEvent {
	return contracts.UplinkEvent{
		EventID:     "188765499021|0x0200|7|1762742400123",
		Phone:       "188765499021",
		MsgID:       0x0200,
		Serial:      7,
		OccurredAt:  time.UnixMilli(1762742400123),
		GatewayID:   "gw-test",
		SessionID:   "session-test",
		Transport:   "tcp",
		Reliability: reliability,
		Payload:     map[string]any{"speed": 42},
		Meta:        map[string]any{"packetCount": 1},
	}
}

func TestBuildHTTPEnvelopeCarriesTerminalReply(t *testing.T) {
	event := testEvent(contracts.ReliabilityBestEffort)
	event.MsgID = 0x0001
	event.Serial = 44
	event.Reply = &contracts.TerminalReply{
		EventID:      event.EventID,
		Phone:        event.Phone,
		AnswerMsgID:  0x8105,
		AnswerSerial: 77,
		Result:       1,
		GatewayID:    event.GatewayID,
		SessionID:    event.SessionID,
		Source:       "http",
	}

	envelope, err := BuildHTTPEnvelope(event)
	require.NoError(t, err)
	require.NotNil(t, envelope.Reply)

	raw, err := json.Marshal(envelope)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"reply":{`)
	require.Contains(t, string(raw), `"answerMsgId":33029`)
	require.Contains(t, string(raw), `"answerSerial":77`)
	require.Contains(t, string(raw), `"result":1`)
}

func newTestSink(t *testing.T, url string, mutate func(*HTTPSinkOptions)) *HTTPSink {
	t.Helper()
	options := HTTPSinkOptions{
		URL:         url,
		Token:       "shared-token",
		Timeout:     time.Second,
		QueueSize:   8,
		Workers:     1,
		MaxAttempts: 1,
	}
	if mutate != nil {
		mutate(&options)
	}
	sink, err := NewHTTPSink(options)
	require.NoError(t, err)
	t.Cleanup(sink.Close)
	return sink
}

func TestNewHTTPSinkRejectsInvalidOptions(t *testing.T) {
	valid := HTTPSinkOptions{
		URL:         "http://127.0.0.1/uplink",
		Token:       "token",
		Timeout:     time.Second,
		QueueSize:   1,
		Workers:     1,
		MaxAttempts: 1,
	}
	cases := []struct {
		name   string
		mutate func(*HTTPSinkOptions)
	}{
		{"missing url", func(o *HTTPSinkOptions) { o.URL = "" }},
		{"missing token", func(o *HTTPSinkOptions) { o.Token = "" }},
		{"zero timeout", func(o *HTTPSinkOptions) { o.Timeout = 0 }},
		{"zero queue", func(o *HTTPSinkOptions) { o.QueueSize = 0 }},
		{"zero workers", func(o *HTTPSinkOptions) { o.Workers = 0 }},
		{"zero attempts", func(o *HTTPSinkOptions) { o.MaxAttempts = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			options := valid
			tc.mutate(&options)
			_, err := NewHTTPSink(options)
			require.Error(t, err)
		})
	}
}

func TestHTTPSinkSendsAuthAndEnvelope(t *testing.T) {
	var got HTTPEnvelope
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer shared-token", r.Header.Get("Authorization"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sink := newTestSink(t, server.URL, nil)
	result, err := sink.Deliver(context.Background(), testEvent(contracts.ReliabilityConfirmBeforeAck))
	require.NoError(t, err)
	require.True(t, result.Accepted)

	require.Equal(t, "188765499021|0x0200|7|1762742400123", got.EventID)
	require.Equal(t, "188765499021", got.Phone)
	require.Equal(t, uint16(0x0200), got.MsgID)
	require.Equal(t, uint16(7), got.Serial)
	require.Equal(t, int64(1762742400123), got.OccurredAtMS)
	require.Equal(t, "gw-test", got.GatewayID)
	require.Equal(t, "session-test", got.SessionID)
	require.Equal(t, "tcp", got.Transport)
	require.Equal(t, string(contracts.ReliabilityConfirmBeforeAck), got.Reliability)
	require.JSONEq(t, `{"speed":42}`, string(got.Payload))
	require.Equal(t, float64(1), got.Meta["packetCount"])

	stats := sink.Stats()
	require.Equal(t, int64(1), stats.Delivered)
	require.Zero(t, stats.Failed)
}

func TestHTTPSinkConfirmNon2xxFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	sink := newTestSink(t, server.URL, nil)
	result, err := sink.Deliver(context.Background(), testEvent(contracts.ReliabilityConfirmBeforeAck))
	require.Error(t, err)
	require.False(t, result.Accepted)
	require.Contains(t, err.Error(), "503")

	stats := sink.Stats()
	require.Equal(t, int64(1), stats.Failed)
	require.Zero(t, stats.Delivered)
}

func TestHTTPSinkConfirmTimeoutFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(500 * time.Millisecond):
		case <-r.Context().Done():
		}
	}))
	defer server.Close()

	sink := newTestSink(t, server.URL, func(o *HTTPSinkOptions) { o.Timeout = 80 * time.Millisecond })
	_, err := sink.Deliver(context.Background(), testEvent(contracts.ReliabilityConfirmBeforeAck))
	require.Error(t, err)

	stats := sink.Stats()
	require.Equal(t, int64(1), stats.Timeouts)
	require.Zero(t, stats.Delivered)
}

func TestHTTPSinkConfirmConnectionFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	sink := newTestSink(t, url, func(o *HTTPSinkOptions) {
		o.Timeout = 200 * time.Millisecond
	})
	_, err := sink.Deliver(context.Background(), testEvent(contracts.ReliabilityConfirmBeforeAck))
	require.Error(t, err)
	require.Equal(t, int64(1), sink.Stats().Failed)
}

func TestHTTPSinkBestEffortRetriesThenSucceeds(t *testing.T) {
	var attempts atomic.Int64
	done := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		select {
		case done <- struct{}{}:
		default:
		}
	}))
	defer server.Close()

	sink := newTestSink(t, server.URL, func(o *HTTPSinkOptions) { o.MaxAttempts = 2 })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sink.Run(ctx)

	result, err := sink.Deliver(context.Background(), testEvent(contracts.ReliabilityBestEffort))
	require.NoError(t, err)
	require.True(t, result.Accepted)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected best-effort delivery to retry and succeed")
	}
	require.Equal(t, int64(2), attempts.Load())
	require.Eventually(t, func() bool {
		return sink.Stats().Delivered == 1
	}, 2*time.Second, 10*time.Millisecond)
}

func TestHTTPSinkBestEffortFailureStillAccepted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	sink := newTestSink(t, server.URL, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sink.Run(ctx)

	result, err := sink.Deliver(context.Background(), testEvent(contracts.ReliabilityBestEffort))
	require.NoError(t, err)
	require.True(t, result.Accepted, "best-effort must not block the protocol acknowledgement")

	require.Eventually(t, func() bool {
		return sink.Stats().Failed == 1
	}, 2*time.Second, 10*time.Millisecond)
}

func TestHTTPSinkBestEffortQueueFullDrops(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sink := newTestSink(t, server.URL, func(o *HTTPSinkOptions) { o.QueueSize = 1 })
	// 不启动 worker，让队列停在容量上限。
	first, err := sink.Deliver(context.Background(), testEvent(contracts.ReliabilityBestEffort))
	require.NoError(t, err)
	require.True(t, first.Accepted)

	second, err := sink.Deliver(context.Background(), testEvent(contracts.ReliabilityBestEffort))
	require.NoError(t, err)
	require.True(t, second.Accepted)

	stats := sink.Stats()
	require.Equal(t, 1, stats.QueueDepth)
	require.Equal(t, int64(1), stats.Dropped)
}

func TestHTTPSinkConfirmRejectsWhenInFlightFull(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sink := newTestSink(t, server.URL, func(o *HTTPSinkOptions) { o.QueueSize = 1 })

	firstDone := make(chan error, 1)
	go func() {
		_, err := sink.Deliver(context.Background(), testEvent(contracts.ReliabilityConfirmBeforeAck))
		firstDone <- err
	}()
	<-entered

	_, err := sink.Deliver(context.Background(), testEvent(contracts.ReliabilityConfirmBeforeAck))
	require.Error(t, err)
	require.Contains(t, err.Error(), "busy")
	require.Equal(t, int64(1), sink.Stats().Rejected)

	close(release)
	require.NoError(t, <-firstDone)
	require.Equal(t, int64(1), sink.Stats().Delivered)
}

func TestHTTPSinkClosedDropsBestEffort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	sink := newTestSink(t, server.URL, nil)
	sink.Close()

	result, err := sink.Deliver(context.Background(), testEvent(contracts.ReliabilityBestEffort))
	require.NoError(t, err)
	require.True(t, result.Accepted)
	require.Equal(t, int64(1), sink.Stats().Dropped)
}

func TestBuildHTTPEnvelopeCarriesRawPayload(t *testing.T) {
	event := testEvent(contracts.ReliabilityBestEffort)
	event.Payload = map[string]any{"multiMediaId": 9}
	event.PayloadRaw = []byte{0x01, 0x02, 0x03}
	event.OccurredAt = time.Time{}

	envelope, err := BuildHTTPEnvelope(event)
	require.NoError(t, err)
	require.Equal(t, int64(0), envelope.OccurredAtMS)
	require.Equal(t, "AQID", envelope.PayloadBase64)
	require.JSONEq(t, `{"multiMediaId":9}`, string(envelope.Payload))
}

func TestBuildHTTPEnvelopeRejectsUnsupportedPayload(t *testing.T) {
	event := testEvent(contracts.ReliabilityBestEffort)
	event.Payload = make(chan int)
	_, err := BuildHTTPEnvelope(event)
	require.Error(t, err)
}
