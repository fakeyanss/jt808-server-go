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
	// OnUnknownDeviceDropped 在 409 重投仍失败时回调，用于指标与告警。
	OnUnknownDeviceDropped func(phone string)
}

// HTTPSinkStats 是 HTTP 出口的累计计数与当前水位。
type HTTPSinkStats struct {
	Delivered  int64
	Failed     int64
	Timeouts   int64
	Dropped    int64
	Rejected   int64
	Unknown    int64
	InFlight   int64
	QueueDepth int
}

// HTTPEnvelope 是上行出口的 JSON 信封，字段与设计文档第 4 节一致。
type HTTPEnvelope struct {
	EventID             string                   `json:"eventId"`
	Phone               string                   `json:"phone"`
	MsgID               uint16                   `json:"msgId"`
	RawMsgID            uint16                   `json:"rawMsgId"`
	Serial              uint16                   `json:"serial"`
	OccurredAtMS        int64                    `json:"occurredAtMs"`
	GatewayID           string                   `json:"gatewayId"`
	SessionID           string                   `json:"sessionId"`
	Transport           string                   `json:"transport"`
	Reliability         string                   `json:"reliability"`
	ProtocolVersion     string                   `json:"protocolVersion,omitempty"`
	HeaderAttr          uint16                   `json:"headerAttr,omitempty"`
	FragmentReassembled bool                     `json:"fragmentReassembled"`
	FragmentTotal       uint16                   `json:"fragmentTotal,omitempty"`
	BodyBase64          string                   `json:"bodyBase64,omitempty"`
	Payload             json.RawMessage          `json:"payload,omitempty"`
	PayloadBase64       string                   `json:"payloadBase64,omitempty"`
	Meta                map[string]any           `json:"meta,omitempty"`
	Reply               *contracts.TerminalReply `json:"reply,omitempty"`
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
	unknown   atomic.Int64
	inFlight  atomic.Int64
	closed    atomic.Bool

	onUnknownDropped func(phone string)
	regMu            sync.Mutex
	registrations    map[string]cachedRegistration
	unknownSince     map[string]time.Time
}

const (
	// registrationEventTTL 是注册事件缓存的有效期；只用于 409 时的一次性重投。
	registrationEventTTL = 10 * time.Minute
	// knownDeviceCooldown 是同一手机号连续 409 的熔断窗口，避免事件反复弹跳。
	knownDeviceCooldown = 30 * time.Second
)

type cachedRegistration struct {
	envelope  HTTPEnvelope
	expiresAt time.Time
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
		slot:             make(chan struct{}, options.QueueSize),
		onUnknownDropped: options.OnUnknownDeviceDropped,
		registrations:    map[string]cachedRegistration{},
		unknownSince:     map[string]time.Time{},
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
		Unknown:    s.unknown.Load(),
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

	if event.MsgID == 0x0100 {
		// 注册事件是 409 重投的唯一依据；即使 best-effort 也要先缓存再投递。
		s.rememberRegistration(envelope)
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
	status, err := s.postWithRetryStatus(ctx, envelope)
	if err == nil {
		return contracts.UplinkResult{Accepted: true}, nil
	}
	if status == http.StatusConflict && envelope.MsgID != 0x0100 {
		return s.handleUnknownDevice(ctx, envelope, err)
	}
	return contracts.UplinkResult{Reason: err.Error()}, err
}

// handleUnknownDevice 处理管控返回的 409。
//
// 边界：
//   - 每个入站事件最多重投一次注册事件；
//   - 重投后仍 409 则熔断 30s，期间同手机号事件直接丢弃并计数；
//   - 丢弃返回错误，网关不应答终端，计入终端自身的重传窗口，不在网关内部循环。
func (s *HTTPSink) handleUnknownDevice(ctx context.Context, envelope HTTPEnvelope, cause error) (contracts.UplinkResult, error) {
	if s.unknownRecently(envelope.Phone) {
		s.dropUnknown(envelope, "device remains unknown within cooldown window")
		return contracts.UplinkResult{Reason: "unknown_device"}, fmt.Errorf("uplink http sink: unknown device (cooldown): %w", cause)
	}
	if registration, ok := s.registration(envelope.Phone); ok && registration.EventID != envelope.EventID {
		if _, retryErr := s.postWithRetryStatus(ctx, registration); retryErr == nil {
			if status, err := s.postWithRetryStatus(ctx, envelope); err == nil {
				return contracts.UplinkResult{Accepted: true}, nil
			} else if status != http.StatusConflict {
				return contracts.UplinkResult{Reason: err.Error()}, err
			}
		}
	}
	s.markUnknown(envelope.Phone)
	s.dropUnknown(envelope, "device is not registered after registration replay")
	return contracts.UplinkResult{Reason: "unknown_device"}, fmt.Errorf("uplink http sink: unknown device: %w", cause)
}

func (s *HTTPSink) dropUnknown(envelope HTTPEnvelope, reason string) {
	s.unknown.Add(1)
	log.Error().
		Str("event_id", envelope.EventID).
		Str("phone", envelope.Phone).
		Str("msg_id", fmt.Sprintf("0x%04X", envelope.MsgID)).
		Msg("Uplink event dropped: " + reason)
	if s.onUnknownDropped != nil {
		s.onUnknownDropped(envelope.Phone)
	}
}

func (s *HTTPSink) rememberRegistration(envelope HTTPEnvelope) {
	s.regMu.Lock()
	defer s.regMu.Unlock()
	now := time.Now()
	if len(s.registrations) > 20000 {
		for phone, entry := range s.registrations {
			if now.After(entry.expiresAt) {
				delete(s.registrations, phone)
			}
		}
	}
	s.registrations[envelope.Phone] = cachedRegistration{envelope: envelope, expiresAt: now.Add(registrationEventTTL)}
}

func (s *HTTPSink) registration(phone string) (HTTPEnvelope, bool) {
	s.regMu.Lock()
	defer s.regMu.Unlock()
	entry, ok := s.registrations[phone]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(s.registrations, phone)
		return HTTPEnvelope{}, false
	}
	return entry.envelope, true
}

func (s *HTTPSink) unknownRecently(phone string) bool {
	s.regMu.Lock()
	defer s.regMu.Unlock()
	since, ok := s.unknownSince[phone]
	return ok && time.Since(since) < knownDeviceCooldown
}

func (s *HTTPSink) markUnknown(phone string) {
	s.regMu.Lock()
	defer s.regMu.Unlock()
	if len(s.unknownSince) > 20000 {
		for key, since := range s.unknownSince {
			if time.Since(since) >= knownDeviceCooldown {
				delete(s.unknownSince, key)
			}
		}
	}
	s.unknownSince[phone] = time.Now()
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
	_, err := s.postWithRetryStatus(ctx, envelope)
	return err
}

// postWithRetryStatus 与 postWithRetry 相同，但把最后一次 HTTP 状态码带回，
// 供 409 unknown_device 判定使用。
func (s *HTTPSink) postWithRetryStatus(ctx context.Context, envelope HTTPEnvelope) (int, error) {
	body, err := json.Marshal(envelope)
	if err != nil {
		s.failed.Add(1)
		return 0, fmt.Errorf("marshal uplink event: %w", err)
	}

	attempts := s.maxAttempts
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	var lastElapsed time.Duration
	lastStatus := 0
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(httpSinkRetryDelay):
			case <-ctx.Done():
				s.recordFailure(envelope, lastErr, lastElapsed)
				return lastStatus, lastErr
			}
		}
		start := time.Now()
		status, err := s.post(ctx, body, envelope.EventID)
		elapsed := time.Since(start)
		lastElapsed = elapsed
		lastStatus = status
		if err == nil && status >= 200 && status < 300 {
			s.delivered.Add(1)
			log.Debug().
				Str("event_id", envelope.EventID).
				Int("status", status).
				Dur("elapsed", elapsed).
				Msg("uplink http sink delivered event")
			return status, nil
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
	return lastStatus, lastErr
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
		EventID:             event.EventID,
		Phone:               event.Phone,
		MsgID:               event.MsgID,
		RawMsgID:            event.RawMsgID,
		Serial:              event.Serial,
		GatewayID:           event.GatewayID,
		SessionID:           event.SessionID,
		Transport:           event.Transport,
		Reliability:         string(event.Reliability),
		ProtocolVersion:     event.ProtocolVersion,
		HeaderAttr:          event.HeaderAttr,
		FragmentReassembled: event.FragmentReassembled,
		FragmentTotal:       event.FragmentTotal,
		Meta:                event.Meta,
		Reply:               event.Reply,
	}
	if !event.OccurredAt.IsZero() {
		envelope.OccurredAtMS = event.OccurredAt.UnixMilli()
	}
	if len(event.Body) > 0 {
		envelope.BodyBase64 = base64.StdEncoding.EncodeToString(event.Body)
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
