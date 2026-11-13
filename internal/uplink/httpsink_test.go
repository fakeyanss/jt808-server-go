package uplink

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
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

// runTestSink 启动分片 worker；confirm-before-ack 现在必须经过队列，
// 因此所有依赖 confirm 结果的测试都要先启动它。
func runTestSink(t *testing.T, sink *HTTPSink) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go sink.Run(ctx)
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
	runTestSink(t, sink)
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
	runTestSink(t, sink)
	result, err := sink.Deliver(context.Background(), testEvent(contracts.ReliabilityConfirmBeforeAck))
	require.Error(t, err)
	require.False(t, result.Accepted)
	require.Contains(t, err.Error(), "503")

	stats := sink.Stats()
	require.Equal(t, int64(1), stats.Failed)
	require.Zero(t, stats.Delivered)
}

// 409 只允许重投一次注册事件；重投后仍失败则丢弃并计数，冷却期内不再重投。
func TestHTTPSinkUnknownDeviceRedeliveryIsBounded(t *testing.T) {
	var registrations, events atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope HTTPEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if envelope.MsgID == 0x0100 {
			registrations.Add(1)
			w.WriteHeader(http.StatusOK)
			return
		}
		events.Add(1)
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()

	var dropped int
	sink := newTestSink(t, server.URL, func(o *HTTPSinkOptions) {
		o.OnUnknownDeviceDropped = func(string) { dropped++ }
	})
	runTestSink(t, sink)

	registration := testEvent(contracts.ReliabilityConfirmBeforeAck)
	registration.MsgID = 0x0100
	registration.EventID = "188765499021|0x0100|1|0"
	_, err := sink.Deliver(context.Background(), registration)
	require.NoError(t, err)

	location := testEvent(contracts.ReliabilityConfirmBeforeAck)
	_, err = sink.Deliver(context.Background(), location)
	require.Error(t, err)
	require.Equal(t, int64(2), registrations.Load(), "registration is replayed exactly once")
	require.Equal(t, int64(2), events.Load(), "non-registration event is retried exactly once")
	require.Equal(t, 1, dropped)

	// 冷却窗口内同一 phone 的事件仍会尝试一次（拿不到 409 就无法判定），
	// 但不再重投注册事件，直接丢弃并计数。
	batch := testEvent(contracts.ReliabilityConfirmBeforeAck)
	batch.MsgID = 0x0704
	batch.EventID = "188765499021|0x0704|8|0"
	_, err = sink.Deliver(context.Background(), batch)
	require.Error(t, err)
	require.Equal(t, int64(2), registrations.Load())
	require.Equal(t, int64(3), events.Load())
	require.Equal(t, 2, dropped)
	require.Equal(t, int64(2), sink.Stats().Unknown)
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
	runTestSink(t, sink)
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
	runTestSink(t, sink)
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
	require.Len(t, stats.ShardQueues, 1)
	require.Equal(t, 1, stats.ShardQueues[0])
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
	runTestSink(t, sink)

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

// 同一 phone 的事件固定落在同一分片，即使 workers > 1 也必须按入队顺序投递。
func TestHTTPSinkSamePhonePreservesOrder(t *testing.T) {
	var mu sync.Mutex
	var order []uint16
	first := make(chan struct{})
	release := make(chan struct{})
	var firstOnce sync.Once

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope HTTPEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		order = append(order, envelope.Serial)
		n := len(order)
		mu.Unlock()
		if n == 1 {
			firstOnce.Do(func() { close(first) })
			<-release
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sink := newTestSink(t, server.URL, func(o *HTTPSinkOptions) {
		o.Workers = 4
		o.QueueSize = 32
	})
	runTestSink(t, sink)

	for serial := uint16(1); serial <= 5; serial++ {
		event := testEvent(contracts.ReliabilityBestEffort)
		event.Serial = serial
		event.EventID = fmt.Sprintf("same-phone|0x0200|%d", serial)
		result, err := sink.Deliver(context.Background(), event)
		require.NoError(t, err)
		require.True(t, result.Accepted)
	}

	select {
	case <-first:
	case <-time.After(2 * time.Second):
		t.Fatal("first event was not delivered")
	}
	close(release)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(order) == 5
	}, 2*time.Second, 10*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []uint16{1, 2, 3, 4, 5}, order)
}

// confirm 事件必须排在同 phone 已入队的 best-effort 事件之后，不能插队。
func TestHTTPSinkConfirmWaitsBehindSamePhoneQueue(t *testing.T) {
	release := make(chan struct{})
	arrived := make(chan uint16, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope HTTPEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		arrived <- envelope.MsgID
		if envelope.MsgID == 0x0200 {
			<-release
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sink := newTestSink(t, server.URL, func(o *HTTPSinkOptions) { o.Workers = 2 })
	runTestSink(t, sink)

	best := testEvent(contracts.ReliabilityBestEffort)
	best.MsgID = 0x0200
	best.EventID = "same-phone|0x0200|1"
	_, err := sink.Deliver(context.Background(), best)
	require.NoError(t, err)
	require.Equal(t, uint16(0x0200), <-arrived)

	confirm := testEvent(contracts.ReliabilityConfirmBeforeAck)
	confirm.MsgID = 0x0102
	confirm.EventID = "same-phone|0x0102|2"
	done := make(chan error, 1)
	go func() {
		_, err := sink.Deliver(context.Background(), confirm)
		done <- err
	}()

	select {
	case <-arrived:
		t.Fatal("confirm overtook the queued best-effort event for the same phone")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-done)
	require.Equal(t, uint16(0x0102), <-arrived)
}

// 不同分片之间仍然并发：两个 phone 分别阻塞在自己的请求里时都应已经进入处理。
func TestHTTPSinkDifferentShardsRunConcurrently(t *testing.T) {
	entered := make(chan string, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope HTTPEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		entered <- envelope.Phone
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sink := newTestSink(t, server.URL, func(o *HTTPSinkOptions) { o.Workers = 2 })
	runTestSink(t, sink)

	var phoneA, phoneB string
	for i := 0; i < 100 && phoneB == ""; i++ {
		candidate := fmt.Sprintf("phone-%d", i)
		if phoneA == "" {
			phoneA = candidate
			continue
		}
		if sink.shardFor(candidate) != sink.shardFor(phoneA) {
			phoneB = candidate
		}
	}
	require.NotEmpty(t, phoneB, "expected to find two phones in different shards")

	for _, phone := range []string{phoneA, phoneB} {
		event := testEvent(contracts.ReliabilityBestEffort)
		event.Phone = phone
		event.EventID = phone + "|0x0200|1"
		result, err := sink.Deliver(context.Background(), event)
		require.NoError(t, err)
		require.True(t, result.Accepted)
	}

	got := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case phone := <-entered:
			got[phone] = true
		case <-time.After(2 * time.Second):
			t.Fatal("different shards did not run concurrently")
		}
	}
	close(release)
	require.True(t, got[phoneA])
	require.True(t, got[phoneB])
}

func TestHTTPSinkConfirmParsesGeneration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"outcome":"accepted","generation":7}`))
	}))
	defer server.Close()

	sink := newTestSink(t, server.URL, nil)
	runTestSink(t, sink)
	result, err := sink.Deliver(context.Background(), testEvent(contracts.ReliabilityConfirmBeforeAck))
	require.NoError(t, err)
	require.True(t, result.Accepted)
	require.Equal(t, uint64(7), result.Generation)
}

func TestHTTPSinkBestEffortFencedClosesSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"outcome":"fenced","fenced":true}`))
	}))
	defer server.Close()

	fenced := make(chan string, 1)
	sink := newTestSink(t, server.URL, func(options *HTTPSinkOptions) {
		options.OnFenced = func(phone, sessionID string) {
			fenced <- phone + "/" + sessionID
		}
	})
	runTestSink(t, sink)
	result, err := sink.Deliver(context.Background(), testEvent(contracts.ReliabilityBestEffort))
	require.NoError(t, err)
	require.True(t, result.Accepted)

	select {
	case got := <-fenced:
		require.Equal(t, "188765499021/session-test", got)
	case <-time.After(2 * time.Second):
		t.Fatal("best-effort fenced response did not trigger OnFenced")
	}
}
