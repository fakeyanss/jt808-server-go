package downlinkhttp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
)

const (
	testToken = "test-token"
	testPhone = "13800000001"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func bindSession(t *testing.T, registry runtime.ConnectionRegistry, phone string) *model.Session {
	t.Helper()
	session := &model.Session{ID: "session-" + phone}
	if err := registry.Bind(context.Background(), session); err != nil {
		t.Fatalf("bind session: %v", err)
	}
	if _, err := registry.BindPhone(context.Background(), phone, session.ID); err != nil {
		t.Fatalf("bind phone: %v", err)
	}
	return session
}

func testMessage(t *testing.T, phone string) (model.JT808Msg, []byte) {
	t.Helper()
	msg := &model.Msg8105{
		Header: &model.MsgHeader{
			MsgID:       0x8105,
			PhoneNumber: phone,
			Attr:        &model.MsgBodyAttr{VersionDesc: model.Version2013},
		},
		CommandWord: 2,
	}
	encoded, err := msg.Encode()
	if err != nil {
		t.Fatalf("encode message: %v", err)
	}
	return msg, encoded
}

func newHarness(t *testing.T, mutate func(*Dependencies)) (*Server, *httptest.Server, *fakeClock, *int32) {
	t.Helper()
	registry := runtime.NewConnectionRegistry()
	bindSession(t, registry, testPhone)
	clock := &fakeClock{now: time.Now()}
	var sends int32
	deps := Dependencies{
		Token:       testToken,
		Timeout:     100 * time.Millisecond,
		IDTTL:       time.Minute,
		MaxInFlight: 4,
		Conn:        registry,
		Send: func(context.Context, string, model.JT808Msg, uint16) error {
			atomic.AddInt32(&sends, 1)
			return nil
		},
		Clock: clock.Now,
	}
	if mutate != nil {
		mutate(&deps)
	}
	server, err := NewServer(deps)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return server, httpServer, clock, &sends
}

func postDownlink(t *testing.T, client *http.Client, url, token string, request downlinkRequest) (*http.Response, []byte) {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	httpRequest, err := http.NewRequest(http.MethodPost, url+PathDownlink, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if token != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	payload, _ := io.ReadAll(response.Body)
	response.Body.Close()
	return response, payload
}

func decodeResponse(t *testing.T, payload []byte) downlinkResponse {
	t.Helper()
	var parsed downlinkResponse
	if err := json.Unmarshal(payload, &parsed); err != nil {
		t.Fatalf("decode response %s: %v", payload, err)
	}
	return parsed
}

func TestSubmitRequiresToken(t *testing.T) {
	_, httpServer, _, sends := newHarness(t, nil)
	_, encoded := testMessage(t, testPhone)
	response, _ := postDownlink(t, http.DefaultClient, httpServer.URL, "", downlinkRequest{
		Phone:         testPhone,
		MessageBase64: base64.StdEncoding.EncodeToString(encoded),
	})
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.StatusCode)
	}
	if atomic.LoadInt32(sends) != 0 {
		t.Fatal("unauthorized request must not send")
	}
}

func TestSubmitOfflineReturns200(t *testing.T) {
	server, httpServer, _, sends := newHarness(t, func(deps *Dependencies) {
		registry := runtime.NewConnectionRegistry()
		deps.Conn = registry
	})
	_ = server
	_, encoded := testMessage(t, testPhone)
	response, payload := postDownlink(t, http.DefaultClient, httpServer.URL, testToken, downlinkRequest{
		Phone:         testPhone,
		MessageBase64: base64.StdEncoding.EncodeToString(encoded),
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.StatusCode, payload)
	}
	if got := decodeResponse(t, payload).Status; got != contracts.DownlinkStatusOffline {
		t.Fatalf("expected offline, got %q", got)
	}
	if atomic.LoadInt32(sends) != 0 {
		t.Fatal("offline request must not send")
	}
}

func TestSubmitAsyncIsIdempotent(t *testing.T) {
	_, httpServer, _, sends := newHarness(t, nil)
	_, encoded := testMessage(t, testPhone)
	request := downlinkRequest{
		Phone:          testPhone,
		IdempotencyKey: "cmd-1",
		Serial:         7,
		MessageBase64:  base64.StdEncoding.EncodeToString(encoded),
	}
	first, firstPayload := postDownlink(t, http.DefaultClient, httpServer.URL, testToken, request)
	if first.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", first.StatusCode, firstPayload)
	}
	second, secondPayload := postDownlink(t, http.DefaultClient, httpServer.URL, testToken, request)
	if second.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on duplicate, got %d: %s", second.StatusCode, secondPayload)
	}
	parsed := decodeResponse(t, secondPayload)
	if !parsed.Duplicated {
		t.Fatal("expected duplicated=true")
	}
	if atomic.LoadInt32(sends) != 1 {
		t.Fatalf("expected one send, got %d", atomic.LoadInt32(sends))
	}
}

func TestSubmitRejectsAsyncCallback(t *testing.T) {
	_, httpServer, _, _ := newHarness(t, nil)
	_, encoded := testMessage(t, testPhone)
	response, payload := postDownlink(t, http.DefaultClient, httpServer.URL, testToken, downlinkRequest{
		Phone:         testPhone,
		Mode:          ModeAsyncCallback,
		MessageBase64: base64.StdEncoding.EncodeToString(encoded),
	})
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", response.StatusCode, payload)
	}
}

func TestQueryUnknownAndExpired(t *testing.T) {
	_, httpServer, clock, _ := newHarness(t, nil)
	_, encoded := testMessage(t, testPhone)
	_, payload := postDownlink(t, http.DefaultClient, httpServer.URL, testToken, downlinkRequest{
		Phone:          testPhone,
		IdempotencyKey: "cmd-expire",
		Serial:         9,
		MessageBase64:  base64.StdEncoding.EncodeToString(encoded),
	})
	created := decodeResponse(t, payload)

	unknown := getDownlink(t, httpServer.URL+PathDownlink+"/missing")
	if unknown.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown id, got %d", unknown.StatusCode)
	}

	clock.advance(2 * time.Minute)
	expired := getDownlink(t, httpServer.URL+PathDownlink+"/"+created.DownlinkID)
	if expired.StatusCode != http.StatusGone {
		t.Fatalf("expected 410 for expired id, got %d", expired.StatusCode)
	}
}

func getDownlink(t *testing.T, url string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	return response
}

func TestSubmitSyncAckWaitsForTerminalResult(t *testing.T) {
	sent := make(chan struct{}, 1)
	server, httpServer, _, _ := newHarness(t, func(deps *Dependencies) {
		deps.Send = func(context.Context, string, model.JT808Msg, uint16) error {
			select {
			case sent <- struct{}{}:
			default:
			}
			return nil
		}
	})
	_, encoded := testMessage(t, testPhone)
	result := make(chan downlinkResponse, 1)
	go func() {
		response, payload := postDownlink(t, http.DefaultClient, httpServer.URL, testToken, downlinkRequest{
			Phone:         testPhone,
			Mode:          ModeSyncAck,
			Serial:        42,
			MessageBase64: base64.StdEncoding.EncodeToString(encoded),
		})
		if response.StatusCode != http.StatusOK {
			result <- downlinkResponse{Reason: string(payload)}
			return
		}
		result <- decodeResponse(t, payload)
	}()

	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("downlink was not sent")
	}
	zero := uint8(0)
	server.HandleReliableResult(model.ReliableResult{
		Phone:          testPhone,
		MsgID:          0x8105,
		Serial:         42,
		Status:         model.ReliableStatusAcked,
		TerminalResult: &zero,
	})
	select {
	case parsed := <-result:
		if parsed.Status != contracts.DownlinkStatusAcked {
			t.Fatalf("expected acked, got %+v", parsed)
		}
	case <-time.After(time.Second):
		t.Fatal("sync_ack did not return after terminal result")
	}
}

// 查询类下行在 ack 后继续等待终端应答体，并在返回前写入 receipt。
func TestSubmitSyncAckReturnsQueryResponse(t *testing.T) {
	sent := make(chan struct{}, 1)
	server, httpServer, _, _ := newHarness(t, func(deps *Dependencies) {
		deps.Send = func(context.Context, string, model.JT808Msg, uint16) error {
			select {
			case sent <- struct{}{}:
			default:
			}
			return nil
		}
	})
	_, encoded := testMessage(t, testPhone)
	result := make(chan downlinkResponse, 1)
	go func() {
		response, payload := postDownlink(t, http.DefaultClient, httpServer.URL, testToken, downlinkRequest{
			Phone:               testPhone,
			Mode:                ModeSyncAck,
			Serial:              52,
			MessageBase64:       base64.StdEncoding.EncodeToString(encoded),
			ExpectResponseMsgID: 0x0104,
			ResponseTimeoutMS:   1000,
		})
		if response.StatusCode != http.StatusOK {
			result <- downlinkResponse{Reason: string(payload)}
			return
		}
		result <- decodeResponse(t, payload)
	}()

	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("downlink was not sent")
	}
	zero := uint8(0)
	server.HandleReliableResult(model.ReliableResult{
		Phone:          testPhone,
		MsgID:          0x8105,
		Serial:         52,
		Status:         model.ReliableStatusAcked,
		TerminalResult: &zero,
	})
	// 0x0104 应答体：应答流水号 52 + 参数个数 0。
	responseBody := []byte{0x00, 0x34, 0x00}
	server.HandleQueryResponse(0x0104, &model.MsgHeader{
		MsgID:        0x0104,
		PhoneNumber:  testPhone,
		SerialNumber: 9,
		Attr:         &model.MsgBodyAttr{},
	}, responseBody)

	select {
	case parsed := <-result:
		if parsed.Status != contracts.DownlinkStatusAcked {
			t.Fatalf("expected acked, got %+v", parsed)
		}
		if parsed.ResponseMsgID != 0x0104 {
			t.Fatalf("expected response msg 0x0104, got 0x%04X", parsed.ResponseMsgID)
		}
		want := base64.StdEncoding.EncodeToString(responseBody)
		if parsed.ResponseBodyBase64 != want {
			t.Fatalf("expected response body %q, got %q", want, parsed.ResponseBodyBase64)
		}
	case <-time.After(time.Second):
		t.Fatal("sync_ack did not return query response")
	}
}

func TestSubmitSyncAckReturns429WhenInFlightFull(t *testing.T) {
	block := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	_, httpServer, _, _ := newHarness(t, func(deps *Dependencies) {
		deps.MaxInFlight = 1
		deps.Timeout = 500 * time.Millisecond
		deps.Send = func(context.Context, string, model.JT808Msg, uint16) error {
			once.Do(func() { close(block) })
			<-release
			return nil
		}
	})
	_, encoded := testMessage(t, testPhone)
	go func() {
		postDownlink(t, http.DefaultClient, httpServer.URL, testToken, downlinkRequest{
			Phone:         testPhone,
			Mode:          ModeSyncAck,
			Serial:        1,
			MessageBase64: base64.StdEncoding.EncodeToString(encoded),
		})
	}()
	select {
	case <-block:
	case <-time.After(time.Second):
		t.Fatal("first sync_ack did not reach send")
	}
	response, payload := postDownlink(t, http.DefaultClient, httpServer.URL, testToken, downlinkRequest{
		Phone:         testPhone,
		Mode:          ModeSyncAck,
		Serial:        2,
		MessageBase64: base64.StdEncoding.EncodeToString(encoded),
	})
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", response.StatusCode, payload)
	}
	close(release)
}

func TestClientSend(t *testing.T) {
	_, httpServer, _, sends := newHarness(t, nil)
	client, err := NewClient(ClientOptions{
		BaseURL: httpServer.URL,
		Token:   testToken,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	msg, _ := testMessage(t, testPhone)
	receipt, err := client.Send(context.Background(), contracts.DownlinkRequest{
		Phone:          testPhone,
		Serial:         11,
		IdempotencyKey: "client-1",
		Message:        msg,
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if receipt.Status != contracts.DownlinkStatusAccepted {
		t.Fatalf("expected accepted, got %+v", receipt)
	}
	if receipt.Serial != 11 {
		t.Fatalf("expected serial 11, got %d", receipt.Serial)
	}
	if atomic.LoadInt32(sends) != 1 {
		t.Fatalf("expected one send, got %d", atomic.LoadInt32(sends))
	}
}

func TestClientRejectsNon2xx(t *testing.T) {
	_, httpServer, _, _ := newHarness(t, nil)
	client, err := NewClient(ClientOptions{
		BaseURL: httpServer.URL,
		Token:   "wrong-token",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	msg, _ := testMessage(t, testPhone)
	if _, err := client.Send(context.Background(), contracts.DownlinkRequest{
		Phone:   testPhone,
		Serial:  12,
		Message: msg,
	}); err == nil {
		t.Fatal("expected error for unauthorized response")
	}
}
