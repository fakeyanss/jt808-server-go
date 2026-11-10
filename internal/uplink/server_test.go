package uplink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

const testControlToken = "control-token"

type fakeConsumer struct {
	calls int32
	err   error
}

func (c *fakeConsumer) ConsumeEvent(context.Context, contracts.UplinkEvent) (contracts.UplinkResult, error) {
	atomic.AddInt32(&c.calls, 1)
	if c.err != nil {
		return contracts.UplinkResult{}, c.err
	}
	return contracts.UplinkResult{Accepted: true}, nil
}

type fakeReplyHandler struct {
	calls   int32
	outcome contracts.TerminalReplyOutcome
	err     error
}

func (h *fakeReplyHandler) HandleTerminalReply(context.Context, contracts.TerminalReply) (contracts.TerminalReplyOutcome, error) {
	atomic.AddInt32(&h.calls, 1)
	if h.err != nil {
		return "", h.err
	}
	if h.outcome == "" {
		return contracts.TerminalReplyAcked, nil
	}
	return h.outcome, nil
}

func newTestServer(t *testing.T, options ServerOptions) (*Server, *httptest.Server) {
	t.Helper()
	if options.Token == "" {
		options.Token = testControlToken
	}
	server, err := NewServer(options)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return server, httpServer
}

func postEnvelope(t *testing.T, url, token string, envelope HTTPEnvelope) (*http.Response, []byte) {
	t.Helper()
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, url+PathUplink, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	payload, _ := readAll(response)
	return response, payload
}

func readAll(response *http.Response) ([]byte, error) {
	defer response.Body.Close()
	buffer := new(bytes.Buffer)
	_, err := buffer.ReadFrom(response.Body)
	return buffer.Bytes(), err
}

func TestUplinkRequiresToken(t *testing.T) {
	_, httpServer := newTestServer(t, ServerOptions{})
	response, _ := postEnvelope(t, httpServer.URL, "", HTTPEnvelope{EventID: "e1", MsgID: 0x0200})
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.StatusCode)
	}
}

func TestUplinkDedupByEventID(t *testing.T) {
	server, httpServer := newTestServer(t, ServerOptions{})
	consumer := &fakeConsumer{}
	server.RegisterConsumer(0x0200, consumer)
	envelope := HTTPEnvelope{
		EventID:     "event-1",
		Phone:       "13800000001",
		MsgID:       0x0200,
		Reliability: string(contracts.ReliabilityBestEffort),
	}
	first, firstPayload := postEnvelope(t, httpServer.URL, testControlToken, envelope)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", first.StatusCode, firstPayload)
	}
	second, secondPayload := postEnvelope(t, httpServer.URL, testControlToken, envelope)
	if second.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", second.StatusCode, secondPayload)
	}
	if !strings.Contains(string(secondPayload), "duplicate") {
		t.Fatalf("expected duplicate outcome, got %s", secondPayload)
	}
	if got := atomic.LoadInt32(&consumer.calls); got != 1 {
		t.Fatalf("expected one consume, got %d", got)
	}
}

func TestUplinkBestEffortWithoutConsumerIsAccepted(t *testing.T) {
	_, httpServer := newTestServer(t, ServerOptions{})
	response, payload := postEnvelope(t, httpServer.URL, testControlToken, HTTPEnvelope{
		EventID:     "event-2",
		MsgID:       0x0200,
		Reliability: string(contracts.ReliabilityBestEffort),
	})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", response.StatusCode, payload)
	}
	if !strings.Contains(string(payload), "unhandled") {
		t.Fatalf("expected unhandled outcome, got %s", payload)
	}
}

func TestUplinkConfirmWithoutConsumerReturns503(t *testing.T) {
	_, httpServer := newTestServer(t, ServerOptions{})
	response, payload := postEnvelope(t, httpServer.URL, testControlToken, HTTPEnvelope{
		EventID:     "event-3",
		MsgID:       0x0801,
		Reliability: string(contracts.ReliabilityConfirmBeforeAck),
	})
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", response.StatusCode, payload)
	}
}

func TestUplinkConsumerFailureKeepsConfirmUnacked(t *testing.T) {
	server, httpServer := newTestServer(t, ServerOptions{})
	server.RegisterConsumer(0x0801, &fakeConsumer{err: fmt.Errorf("db down")})
	response, payload := postEnvelope(t, httpServer.URL, testControlToken, HTTPEnvelope{
		EventID:     "event-4",
		MsgID:       0x0801,
		Reliability: string(contracts.ReliabilityConfirmBeforeAck),
	})
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", response.StatusCode, payload)
	}
}

// TestUplinkConfirmRetryAfterFailureIsNotDuplicate 覆盖 confirm-before-ack 的重试语义：
// 处理失败返回 503 时不得记录 eventId，否则网关重试会被当成 duplicate 而误应答终端。
func TestUplinkConfirmRetryAfterFailureIsNotDuplicate(t *testing.T) {
	server, httpServer := newTestServer(t, ServerOptions{})
	consumer := &fakeConsumer{err: fmt.Errorf("db down")}
	server.RegisterConsumer(0x0801, consumer)
	envelope := HTTPEnvelope{
		EventID:     "event-retry",
		MsgID:       0x0801,
		Reliability: string(contracts.ReliabilityConfirmBeforeAck),
	}

	first, firstPayload := postEnvelope(t, httpServer.URL, testControlToken, envelope)
	if first.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", first.StatusCode, firstPayload)
	}

	consumer.err = nil
	second, secondPayload := postEnvelope(t, httpServer.URL, testControlToken, envelope)
	if second.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on retry, got %d: %s", second.StatusCode, secondPayload)
	}
	if strings.Contains(string(secondPayload), "duplicate") {
		t.Fatalf("retry must be reprocessed, got %s", secondPayload)
	}
	if got := atomic.LoadInt32(&consumer.calls); got != 2 {
		t.Fatalf("expected two consumes, got %d", got)
	}

	third, thirdPayload := postEnvelope(t, httpServer.URL, testControlToken, envelope)
	if third.StatusCode != http.StatusOK || !strings.Contains(string(thirdPayload), "duplicate") {
		t.Fatalf("expected duplicate after success, got %d: %s", third.StatusCode, thirdPayload)
	}
	if got := atomic.LoadInt32(&consumer.calls); got != 2 {
		t.Fatalf("expected consumes to stay at 2, got %d", got)
	}
}

func TestUplinkRoutesTerminalReply(t *testing.T) {
	server, httpServer := newTestServer(t, ServerOptions{})
	handler := &fakeReplyHandler{outcome: contracts.TerminalReplyAcked}
	server.SetTerminalReplyHandler(handler)
	response, payload := postEnvelope(t, httpServer.URL, testControlToken, HTTPEnvelope{
		EventID: "event-5",
		MsgID:   0x0001,
		Reply: &contracts.TerminalReply{
			EventID:      "event-5",
			Phone:        "13800000001",
			AnswerMsgID:  0x8300,
			AnswerSerial: 3,
			Result:       0,
		},
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.StatusCode, payload)
	}
	if !strings.Contains(string(payload), string(contracts.TerminalReplyAcked)) {
		t.Fatalf("expected acked outcome, got %s", payload)
	}
	if got := atomic.LoadInt32(&handler.calls); got != 1 {
		t.Fatalf("expected one reply, got %d", got)
	}
}

func TestUplinkReplyWithoutHandlerReturns503(t *testing.T) {
	_, httpServer := newTestServer(t, ServerOptions{})
	response, payload := postEnvelope(t, httpServer.URL, testControlToken, HTTPEnvelope{
		EventID: "event-6",
		MsgID:   0x0001,
		Reply: &contracts.TerminalReply{
			Phone:        "13800000001",
			AnswerMsgID:  0x8300,
			AnswerSerial: 3,
		},
	})
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", response.StatusCode, payload)
	}
}

func TestUplinkRejectsOversizedBody(t *testing.T) {
	_, httpServer := newTestServer(t, ServerOptions{MaxBodyBytes: 64})
	response, payload := postEnvelope(t, httpServer.URL, testControlToken, HTTPEnvelope{
		EventID: "event-7",
		MsgID:   0x0200,
		Meta:    map[string]any{"padding": strings.Repeat("x", 512)},
	})
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", response.StatusCode, payload)
	}
}

func TestValidateCoverage(t *testing.T) {
	server, _ := newTestServer(t, ServerOptions{})
	policy := NewPolicy("best_effort", "best_effort", "best_effort", "best_effort")
	err := server.ValidateCoverage(policy)
	if err == nil {
		t.Fatal("expected coverage error")
	}
	for _, want := range []string{"0x0001", "0x0801"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %s in coverage error, got %v", want, err)
		}
	}

	server.RegisterConsumer(0x0801, &fakeConsumer{})
	server.SetTerminalReplyHandler(&fakeReplyHandler{})
	if err := server.ValidateCoverage(policy); err != nil {
		t.Fatalf("expected coverage to pass, got %v", err)
	}

	atLeastOnce := NewPolicy("best_effort", "at_least_once", "at_least_once", "at_least_once")
	err = server.ValidateCoverage(atLeastOnce)
	if err == nil {
		t.Fatal("expected coverage error for at_least_once categories")
	}
	for _, want := range []string{"0x0200", "0x0704"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %s in coverage error, got %v", want, err)
		}
	}
	server.RegisterConsumer(0x0200, &fakeConsumer{})
	server.RegisterConsumer(0x0704, &fakeConsumer{})
	if err := server.ValidateCoverage(atLeastOnce); err != nil {
		t.Fatalf("expected coverage to pass, got %v", err)
	}
}
