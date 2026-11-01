package action

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

const (
	defaultHTTPTimeout   = 2 * time.Second
	defaultHTTPQueueSize = 256
)

// HTTPCallback 是一个示例 Action 实现：把生命周期事件以 JSON 异步 POST 到配置的 URL。
// 队列满时丢弃事件，HTTP 失败只记日志，均不会阻塞协议处理。
type HTTPCallback struct {
	url     string
	timeout time.Duration
	client  *http.Client
	events  chan httpEvent
}

type httpEvent struct {
	Event      string      `json:"event"`
	OccurredAt time.Time   `json:"occurredAt"`
	Payload    interface{} `json:"payload"`
}

// NewHTTPCallback 创建 HTTP 回调实现。timeout <= 0 时使用默认 2 秒。
func NewHTTPCallback(url string, timeout time.Duration) *HTTPCallback {
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	cb := &HTTPCallback{
		url:     url,
		timeout: timeout,
		client:  &http.Client{Timeout: timeout},
		events:  make(chan httpEvent, defaultHTTPQueueSize),
	}
	routines.GoSafe(cb.loop)
	return cb
}

func (cb *HTTPCallback) OnRegister(ctx context.Context, event RegisterEvent) {
	cb.enqueue("register", event)
}

func (cb *HTTPCallback) OnAuth(ctx context.Context, event AuthEvent) {
	cb.enqueue("auth", event)
}

func (cb *HTTPCallback) OnLocation(ctx context.Context, event LocationEvent) {
	cb.enqueue("location", event)
}

func (cb *HTTPCallback) OnAlarm(ctx context.Context, event AlarmEvent) {
	cb.enqueue("alarm", event)
}

func (cb *HTTPCallback) OnDisconnect(ctx context.Context, event DisconnectEvent) {
	cb.enqueue("disconnect", event)
}

func (cb *HTTPCallback) enqueue(name string, payload interface{}) {
	if cb == nil || cb.url == "" {
		return
	}
	select {
	case cb.events <- httpEvent{Event: name, OccurredAt: time.Now(), Payload: payload}:
	default:
		log.Warn().Str("event", name).Msg("Action HTTP callback queue is full, dropping event")
	}
}

func (cb *HTTPCallback) loop() {
	for event := range cb.events {
		cb.post(event)
	}
}

func (cb *HTTPCallback) post(event httpEvent) {
	body, err := json.Marshal(event)
	if err != nil {
		log.Error().Err(err).Str("event", event.Event).Msg("Failed to marshal action event")
		return
	}
	req, err := http.NewRequest(http.MethodPost, cb.url, bytes.NewReader(body))
	if err != nil {
		log.Error().Err(err).Str("event", event.Event).Msg("Failed to build action HTTP request")
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := cb.client.Do(req)
	if err != nil {
		log.Warn().Err(err).Str("event", event.Event).Str("url", cb.url).Msg("Action HTTP callback failed")
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		log.Warn().Int("status", resp.StatusCode).Str("event", event.Event).Str("url", cb.url).Msg("Action HTTP callback returned error status")
	}
}
