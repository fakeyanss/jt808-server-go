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

	"github.com/fakeyanss/jt808-server-go/internal/codec/hash"
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
	// OnFenced 在 best-effort 响应携带 fenced 时回调；网关据此关闭本地会话。
	// confirm 路径通过 UplinkResult.Fenced 同步返回，不走该回调。
	OnFenced func(phone, sessionID string)
}

// HTTPSinkStats 是 HTTP 出口的累计计数与当前水位。
type HTTPSinkStats struct {
	Delivered int64
	Failed    int64
	Timeouts  int64
	Dropped   int64
	Rejected  int64
	Unknown   int64
	InFlight  int64
	// ShardQueues 是每个 phone 分片队列的当前深度，下标即分片号。
	// 出口按 phone 哈希分片、分片内单 goroutine 顺序消费，因此不再存在
	// 能代表整体拓扑的单一队列深度。
	ShardQueues []int
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
	InstanceID          string                   `json:"instanceId,omitempty"`
	SessionID           string                   `json:"sessionId"`
	Generation          uint64                   `json:"generation,omitempty"`
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
//
// 队列按 phone 分片：同一 phone 的事件进入同一分片，由单个 goroutine
// 顺序消费；不同分片之间并发。confirm 事件也走同一分片队列，保证不会
// 越过同 phone 已排队的事件。
type HTTPSink struct {
	url         string
	token       string
	timeout     time.Duration
	maxAttempts int
	client      *http.Client

	shards   []chan sinkJob
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
	onFenced         func(phone, sessionID string)
	regMu            sync.Mutex
	registrations    map[string]cachedRegistration
	unknownSince     map[string]time.Time
}

// sinkJob 是一次分片内投递任务。
// result 非空表示 confirm-before-ack：worker 完成后把结果写回，调用方等待。
type sinkJob struct {
	envelope HTTPEnvelope
	result   chan sinkResult
}

type sinkResult struct {
	result contracts.UplinkResult
	err    error
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
	// 分片数直接复用 workers：分片内单 goroutine 顺序消费。
	// 每个分片独立有界，总量仍以 queueSize 为预算（向下取整，至少 1）。
	shardCap := options.QueueSize / options.Workers
	if shardCap < 1 {
		shardCap = 1
	}
	shards := make([]chan sinkJob, options.Workers)
	for i := range shards {
		shards[i] = make(chan sinkJob, shardCap)
	}
	return &HTTPSink{
		url:         options.URL,
		token:       options.Token,
		timeout:     options.Timeout,
		maxAttempts: options.MaxAttempts,
		client: &http.Client{
			Timeout: options.Timeout,
		},
		shards: shards,
		done:   make(chan struct{}),
		// confirm-before-ack 的并发上限与队列容量一致，避免无界 goroutine。
		slot:             make(chan struct{}, options.QueueSize),
		onUnknownDropped: options.OnUnknownDeviceDropped,
		onFenced:         options.OnFenced,
		registrations:    map[string]cachedRegistration{},
		unknownSince:     map[string]time.Time{},
	}, nil
}

// Run 启动各分片 worker，并在 ctx 结束时停止接收新事件。
func (s *HTTPSink) Run(ctx context.Context) {
	if s == nil {
		return
	}
	s.startOne.Do(func() {
		var wg sync.WaitGroup
		for _, jobs := range s.shards {
			wg.Add(1)
			go func(jobs <-chan sinkJob) {
				defer wg.Done()
				s.worker(ctx, jobs)
			}(jobs)
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

// Stats 返回当前累计计数与各分片队列水位。
func (s *HTTPSink) Stats() HTTPSinkStats {
	if s == nil {
		return HTTPSinkStats{}
	}
	shardQueues := make([]int, len(s.shards))
	for i, jobs := range s.shards {
		shardQueues[i] = len(jobs)
	}
	return HTTPSinkStats{
		Delivered:   s.delivered.Load(),
		Failed:      s.failed.Load(),
		Timeouts:    s.timeouts.Load(),
		Dropped:     s.dropped.Load(),
		Rejected:    s.rejected.Load(),
		Unknown:     s.unknown.Load(),
		InFlight:    s.inFlight.Load(),
		ShardQueues: shardQueues,
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

// shardFor 按 phone 选择分片；同一 phone 永远命中同一分片，从而由同一条
// goroutine 顺序处理。不同 phone 只有在哈希碰撞时才共享分片。
func (s *HTTPSink) shardFor(phone string) chan sinkJob {
	return s.shards[int(hash.FNV32(phone)%uint32(len(s.shards)))]
}

func (s *HTTPSink) enqueue(envelope HTTPEnvelope) (contracts.UplinkResult, error) {
	jobs := s.shardFor(envelope.Phone)
	if s.closed.Load() {
		s.dropped.Add(1)
		log.Warn().Str("event_id", envelope.EventID).Msg("uplink http sink dropped event: sink closed")
		return contracts.UplinkResult{Accepted: true}, nil
	}
	select {
	case jobs <- sinkJob{envelope: envelope}:
		log.Debug().
			Str("event_id", envelope.EventID).
			Int("shard_queue_depth", len(jobs)).
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
			Int("shard_queue_depth", len(jobs)).
			Str("reliability", envelope.Reliability).
			Msg("uplink http sink dropped event: shard queue full")
		return contracts.UplinkResult{Accepted: true}, nil
	}
}

func (s *HTTPSink) deliverSync(ctx context.Context, envelope HTTPEnvelope) (contracts.UplinkResult, error) {
	if s.closed.Load() {
		s.rejected.Add(1)
		return contracts.UplinkResult{Reason: "sink closed"}, fmt.Errorf("uplink http sink closed")
	}
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

	// confirm 事件与 best-effort 走同一分片队列，只是额外等待结果；
	// 这样它不会越过同 phone 已排队的事件。
	resultCh := make(chan sinkResult, 1)
	jobs := s.shardFor(envelope.Phone)
	select {
	case jobs <- sinkJob{envelope: envelope, result: resultCh}:
	case <-s.done:
		s.rejected.Add(1)
		return contracts.UplinkResult{Reason: "sink closed"}, fmt.Errorf("uplink http sink closed")
	default:
		s.rejected.Add(1)
		log.Warn().
			Str("event_id", envelope.EventID).
			Int("shard_queue_depth", len(jobs)).
			Msg("uplink http sink rejected event: shard queue full")
		return contracts.UplinkResult{Reason: "too many in-flight requests"}, fmt.Errorf("uplink http sink busy")
	}

	select {
	case result := <-resultCh:
		return result.result, result.err
	case <-ctx.Done():
		return contracts.UplinkResult{Reason: ctx.Err().Error()}, ctx.Err()
	case <-s.done:
		return contracts.UplinkResult{Reason: "sink closed"}, fmt.Errorf("uplink http sink closed")
	}
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
		if _, _, retryErr := s.postWithRetryStatus(ctx, registration); retryErr == nil {
			if status, resp, err := s.postWithRetryStatus(ctx, envelope); err == nil {
				return contracts.UplinkResult{
					Accepted:   true,
					Generation: resp.Generation,
					Fenced:     resp.Fenced,
				}, nil
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

func (s *HTTPSink) worker(ctx context.Context, jobs <-chan sinkJob) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case job := <-jobs:
			s.runJob(job)
		}
	}
}

// runJob 在分片内顺序执行一次投递。
// HTTP 请求不继承连接 ctx：任务一旦入队就应处理完，连接断开不会取消已
// 接收的事件；confirm 调用方可能提前返回，但事件仍按序投递（at-least-once）。
func (s *HTTPSink) runJob(job sinkJob) {
	postCtx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	if job.result == nil {
		resp, err := s.postWithRetry(postCtx, job.envelope)
		if err != nil {
			log.Warn().Err(err).Str("event_id", job.envelope.EventID).Msg("uplink http sink best-effort delivery failed")
		} else if resp.Fenced {
			s.notifyFenced(job.envelope)
		}
		return
	}

	s.inFlight.Add(1)
	defer s.inFlight.Add(-1)
	result, err := s.deliverConfirm(postCtx, job.envelope)
	job.result <- sinkResult{result: result, err: err}
}

func (s *HTTPSink) deliverConfirm(ctx context.Context, envelope HTTPEnvelope) (contracts.UplinkResult, error) {
	status, resp, err := s.postWithRetryStatus(ctx, envelope)
	if err == nil {
		return contracts.UplinkResult{
			Accepted:   true,
			Generation: resp.Generation,
			Fenced:     resp.Fenced,
		}, nil
	}
	if status == http.StatusConflict && envelope.MsgID != 0x0100 {
		return s.handleUnknownDevice(ctx, envelope, err)
	}
	return contracts.UplinkResult{Reason: err.Error()}, err
}

func (s *HTTPSink) postWithRetry(ctx context.Context, envelope HTTPEnvelope) (controlResponse, error) {
	_, resp, err := s.postWithRetryStatus(ctx, envelope)
	return resp, err
}

// postWithRetryStatus 与 postWithRetry 相同，但把最后一次 HTTP 状态码带回，
// 供 409 unknown_device 判定使用。
// controlResponse 是 control 上行入口返回的最小结果，confirm 与 best-effort 共用。
type controlResponse struct {
	Outcome    string `json:"outcome"`
	Generation uint64 `json:"generation"`
	Fenced     bool   `json:"fenced"`
}

func (s *HTTPSink) postWithRetryStatus(ctx context.Context, envelope HTTPEnvelope) (int, controlResponse, error) {
	body, err := json.Marshal(envelope)
	if err != nil {
		s.failed.Add(1)
		return 0, controlResponse{}, fmt.Errorf("marshal uplink event: %w", err)
	}

	attempts := s.maxAttempts
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	var lastResp controlResponse
	var lastElapsed time.Duration
	lastStatus := 0
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(httpSinkRetryDelay):
			case <-ctx.Done():
				s.recordFailure(envelope, lastErr, lastElapsed)
				return lastStatus, lastResp, lastErr
			}
		}
		start := time.Now()
		status, resp, err := s.post(ctx, body, envelope.EventID)
		elapsed := time.Since(start)
		lastElapsed = elapsed
		lastStatus = status
		lastResp = resp
		if err == nil && status >= 200 && status < 300 {
			s.delivered.Add(1)
			log.Debug().
				Str("event_id", envelope.EventID).
				Int("status", status).
				Dur("elapsed", elapsed).
				Msg("uplink http sink delivered event")
			return status, resp, nil
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
	return lastStatus, lastResp, lastErr
}

func (s *HTTPSink) post(ctx context.Context, body []byte, eventID string) (int, controlResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return 0, controlResponse{}, fmt.Errorf("build uplink request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+s.token)
	request.Header.Set("X-Event-Id", eventID)

	response, err := s.client.Do(request)
	if err != nil {
		return 0, controlResponse{}, err
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	var resp controlResponse
	if len(payload) > 0 {
		// 响应体只承载 generation/fenced 这类提示；解析失败不影响状态码语义。
		_ = json.Unmarshal(payload, &resp)
	}
	return response.StatusCode, resp, nil
}

// notifyFenced 处理 best-effort 响应里的 fenced：记录并关闭本地会话。
func (s *HTTPSink) notifyFenced(envelope HTTPEnvelope) {
	log.Warn().
		Str("event_id", envelope.EventID).
		Str("phone", envelope.Phone).
		Str("session_id", envelope.SessionID).
		Uint16("msg_id", envelope.MsgID).
		Msg("Uplink event fenced by control; closing local session")
	if s.onFenced != nil {
		s.onFenced(envelope.Phone, envelope.SessionID)
	}
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
		InstanceID:          event.InstanceID,
		SessionID:           event.SessionID,
		Generation:          event.Generation,
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
