package uplink

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

const (
	// httpSinkRetryDelay 是同一事件两次投递之间的固定间隔。
	// 只用于连接存活期内的少量重试，不构成持久重投。
	httpSinkRetryDelay = 50 * time.Millisecond
)

// HTTPSinkOptions 是 HTTP 上行出口的构造参数。
type HTTPSinkOptions struct {
	URL         string
	Token       string
	Timeout     time.Duration
	QueueSize   int
	Workers     int
	MaxAttempts int
}

// HTTPSinkStats 是 HTTP 出口的累计计数与当前水位。
type HTTPSinkStats struct {
	Delivered  int64
	Failed     int64
	Timeouts   int64
	Dropped    int64
	Rejected   int64
	InFlight   int64
	QueueDepth int
}

// HTTPEnvelope 是上行出口的 JSON 信封，字段与设计文档第 4 节一致。
type HTTPEnvelope struct {
	EventID       string                   `json:"eventId"`
	Phone         string                   `json:"phone"`
	MsgID         uint16                   `json:"msgId"`
	Serial        uint16                   `json:"serial"`
	OccurredAtMS  int64                    `json:"occurredAtMs"`
	GatewayID     string                   `json:"gatewayId"`
	SessionID     string                   `json:"sessionId"`
	Transport     string                   `json:"transport"`
	Reliability   string                   `json:"reliability"`
	Payload       json.RawMessage          `json:"payload,omitempty"`
	PayloadBase64 string                   `json:"payloadBase64,omitempty"`
	Meta          map[string]any           `json:"meta,omitempty"`
	Reply         *contracts.TerminalReply `json:"reply,omitempty"`
}

// HTTPSink 把上行事件通过 HTTP hook 投递给管控。
//
// 两类语义：
//   - best-effort：事件进入有界队列，立即返回；队列满直接丢弃并计数；
//   - confirm-before-ack：同步 POST，成功才返回 nil；失败/超时返回错误，
//     调用方因此不应答终端，由终端按协议重传。
type HTTPSink struct {
	url         string
	token       string
	timeout     time.Duration
	maxAttempts int
	client      *http.Client

	queue    chan HTTPEnvelope
	workers  int
	done     chan struct{}
	startOne sync.Once
	stopOne  sync.Once
	slot     chan struct{}

	delivered atomic.Int64
	failed    atomic.Int64
	timeouts  atomic.Int64
	dropped   atomic.Int64
	rejected  atomic.Int64
	inFlight  atomic.Int64
	closed    atomic.Bool
}

// NewHTTPSink 构造 HTTP 上行出口；URL、凭据与边界参数非法时返回错误。
func NewHTTPSink(options HTTPSinkOptions) (*HTTPSink, error) {
	if options.URL == "" {
		return nil, fmt.Errorf("uplink http sink: url is required")
	}
	if options.Token == "" {
		return nil, fmt.Errorf("uplink http sink: token is required")
	}
	if options.Timeout <= 0 {
		return nil, fmt.Errorf("uplink http sink: timeout must be > 0")
	}
	if options.QueueSize <= 0 {
		return nil, fmt.Errorf("uplink http sink: queueSize must be > 0")
	}
	if options.Workers <= 0 {
		return nil, fmt.Errorf("uplink http sink: workers must be > 0")
	}
	if options.MaxAttempts <= 0 {
		return nil, fmt.Errorf("uplink http sink: maxAttempts must be > 0")
	}
	return &HTTPSink{
		url:         options.URL,
		token:       options.Token,
		timeout:     options.Timeout,
		maxAttempts: options.MaxAttempts,
		client: &http.Client{
			Timeout: options.Timeout,
		},
		queue:   make(chan HTTPEnvelope, options.QueueSize),
		workers: options.Workers,
		done:    make(chan struct{}),
		// confirm-before-ack 的并发上限与队列容量一致，避免无界 goroutine。
		slot: make(chan struct{}, options.QueueSize),
	}, nil
}

// Run 启动 best-effort worker，并在 ctx 结束时停止接收新事件。
func (s *HTTPSink) Run(ctx context.Context) {
	if s == nil {
		return
	}
	s.startOne.Do(func() {
		var wg sync.WaitGroup
		for i := 0; i < s.workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.worker(ctx)
			}()
		}
		<-ctx.Done()
		s.stop()
		wg.Wait()
	})
}

// Close 停止接收新事件；已入队事件由 worker 按 ctx 状态处理。
func (s *HTTPSink) Close() {
	if s == nil {
		return
	}
	s.stop()
}

func (s *HTTPSink) stop() {
	s.stopOne.Do(func() {
		s.closed.Store(true)
		close(s.done)
	})
}

// Stats 返回当前累计计数与队列水位。
func (s *HTTPSink) Stats() HTTPSinkStats {
	if s == nil {
		return HTTPSinkStats{}
	}
	return HTTPSinkStats{
		Delivered:  s.delivered.Load(),
		Failed:     s.failed.Load(),
		Timeouts:   s.timeouts.Load(),
		Dropped:    s.dropped.Load(),
		Rejected:   s.rejected.Load(),
		InFlight:   s.inFlight.Load(),
		QueueDepth: len(s.queue),
	}
}

// Deliver 按事件的可靠语义投递；confirm-before-ack 失败时返回错误。
func (s *HTTPSink) Deliver(ctx context.Context, event contracts.UplinkEvent) (contracts.UplinkResult, error) {
	if s == nil {
		return contracts.UplinkResult{}, fmt.Errorf("uplink http sink is not configured")
	}
	envelope, err := BuildHTTPEnvelope(event)
	if err != nil {
		if event.Reliability == contracts.ReliabilityConfirmBeforeAck {
			s.failed.Add(1)
			return contracts.UplinkResult{Reason: err.Error()}, err
		}
		s.dropped.Add(1)
		log.Warn().Err(err).Str("event_id", event.EventID).Msg("uplink http sink dropped event: encode failed")
		return contracts.UplinkResult{Accepted: true}, nil
	}

	if event.Reliability != contracts.ReliabilityConfirmBeforeAck {
		return s.enqueue(envelope)
	}
	return s.deliverSync(ctx, envelope)
}

func (s *HTTPSink) enqueue(envelope HTTPEnvelope) (contracts.UplinkResult, error) {
	if s.closed.Load() {
		s.dropped.Add(1)
		log.Warn().Str("event_id", envelope.EventID).Int("queue_depth", len(s.queue)).Msg("uplink http sink dropped event: sink closed")
		return contracts.UplinkResult{Accepted: true}, nil
	}
	select {
	case s.queue <- envelope:
		log.Debug().
			Str("event_id", envelope.EventID).
			Int("queue_depth", len(s.queue)).
			Msg("uplink http sink enqueued event")
		return contracts.UplinkResult{Accepted: true}, nil
	case <-s.done:
		s.dropped.Add(1)
		log.Warn().Str("event_id", envelope.EventID).Msg("uplink http sink dropped event: sink closed")
		return contracts.UplinkResult{Accepted: true}, nil
	default:
		s.dropped.Add(1)
		log.Warn().
			Str("event_id", envelope.EventID).
			Int("queue_depth", len(s.queue)).
			Str("reliability", envelope.Reliability).
			Msg("uplink http sink dropped event: queue full")
		return contracts.UplinkResult{Accepted: true}, nil
	}
}

func (s *HTTPSink) deliverSync(ctx context.Context, envelope HTTPEnvelope) (contracts.UplinkResult, error) {
	select {
	case s.slot <- struct{}{}:
		defer func() { <-s.slot }()
	case <-s.done:
		s.rejected.Add(1)
		return contracts.UplinkResult{Reason: "sink closed"}, fmt.Errorf("uplink http sink closed")
	default:
		s.rejected.Add(1)
		log.Warn().
			Str("event_id", envelope.EventID).
			Int64("in_flight", s.inFlight.Load()).
			Msg("uplink http sink rejected event: too many in-flight requests")
		return contracts.UplinkResult{Reason: "too many in-flight requests"}, fmt.Errorf("uplink http sink busy")
	}

	s.inFlight.Add(1)
	defer s.inFlight.Add(-1)
	err := s.postWithRetry(ctx, envelope)
	if err != nil {
		return contracts.UplinkResult{Reason: err.Error()}, err
	}
	return contracts.UplinkResult{Accepted: true}, nil
}

func (s *HTTPSink) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case envelope := <-s.queue:
			// worker 侧不继承连接 ctx 的取消作为失败原因：队列在关闭时可能仍有积压，
			// 但只处理到进程退出为止，不新建持久重投。
			postCtx, cancel := context.WithTimeout(context.Background(), s.timeout)
			if err := s.postWithRetry(postCtx, envelope); err != nil {
				log.Warn().Err(err).Str("event_id", envelope.EventID).Msg("uplink http sink best-effort delivery failed")
			}
			cancel()
		}
	}
}

func (s *HTTPSink) postWithRetry(ctx context.Context, envelope HTTPEnvelope) error {
	body, err := json.Marshal(envelope)
	if err != nil {
		s.failed.Add(1)
		return fmt.Errorf("marshal uplink event: %w", err)
	}

	attempts := s.maxAttempts
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	var lastElapsed time.Duration
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(httpSinkRetryDelay):
			case <-ctx.Done():
				s.recordFailure(envelope, lastErr, lastElapsed)
				return lastErr
			}
		}
		start := time.Now()
		status, err := s.post(ctx, body, envelope.EventID)
		elapsed := time.Since(start)
		lastElapsed = elapsed
		if err == nil && status >= 200 && status < 300 {
			s.delivered.Add(1)
			log.Debug().
				Str("event_id", envelope.EventID).
				Int("status", status).
				Dur("elapsed", elapsed).
				Msg("uplink http sink delivered event")
			return nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("uplink http sink returned status %d", status)
		}
		if ctx.Err() != nil {
			break
		}
	}

	s.recordFailure(envelope, lastErr, lastElapsed)
	return lastErr
}

func (s *HTTPSink) post(ctx context.Context, body []byte, eventID string) (int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("build uplink request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+s.token)
	request.Header.Set("X-Event-Id", eventID)

	response, err := s.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return response.StatusCode, nil
}

func (s *HTTPSink) recordFailure(envelope HTTPEnvelope, err error, elapsed time.Duration) {
	if err != nil && (isTimeout(err) || errIsDeadline(err)) {
		s.timeouts.Add(1)
		log.Warn().Err(err).Str("event_id", envelope.EventID).Dur("elapsed", elapsed).Msg("uplink http sink timed out")
		return
	}
	s.failed.Add(1)
	log.Warn().Err(err).Str("event_id", envelope.EventID).Dur("elapsed", elapsed).Msg("uplink http sink delivery failed")
}

func isTimeout(err error) bool {
	type timeout interface{ Timeout() bool }
	te, ok := err.(timeout)
	return ok && te.Timeout()
}

func errIsDeadline(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

// BuildHTTPEnvelope 把契约事件转换为线上 JSON 信封。
func BuildHTTPEnvelope(event contracts.UplinkEvent) (HTTPEnvelope, error) {
	envelope := HTTPEnvelope{
		EventID:     event.EventID,
		Phone:       event.Phone,
		MsgID:       event.MsgID,
		Serial:      event.Serial,
		GatewayID:   event.GatewayID,
		SessionID:   event.SessionID,
		Transport:   event.Transport,
		Reliability: string(event.Reliability),
		Meta:        event.Meta,
		Reply:       event.Reply,
	}
	if !event.OccurredAt.IsZero() {
		envelope.OccurredAtMS = event.OccurredAt.UnixMilli()
	}
	if event.Payload != nil {
		payload, err := json.Marshal(event.Payload)
		if err != nil {
			return HTTPEnvelope{}, fmt.Errorf("marshal uplink payload: %w", err)
		}
		envelope.Payload = payload
	}
	if len(event.PayloadRaw) > 0 {
		envelope.PayloadBase64 = base64.StdEncoding.EncodeToString(event.PayloadRaw)
	}
	return envelope, nil
}
