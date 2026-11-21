package uplink

import (
	"bytes"
	"context"
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
	"github.com/fakeyanss/jt808-server-go/internal/internalapi"
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
	// Attempts 按分类覆盖 MaxAttempts；零值字段回落到 MaxAttempts。
	Attempts AttemptOverrides
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
	InFlight  int64
	// ShardQueues 是每个 phone 分片队列的当前深度，下标即分片号。
	// 出口按 phone 哈希分片、分片内单 goroutine 顺序消费，因此不再存在
	// 能代表整体拓扑的单一队列深度。
	ShardQueues []int
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
	url      string
	token    string
	timeout  time.Duration
	attempts AttemptPolicy
	client   *http.Client

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
	inFlight  atomic.Int64
	closed    atomic.Bool

	onFenced func(phone, sessionID string)
}

// sinkJob 是一次分片内投递任务。
// result 非空表示 confirm-before-ack：worker 完成后把结果写回，调用方等待。
type sinkJob struct {
	envelope internalapi.UplinkEnvelope
	// attempts 是这条事件本次投递的次数上限（含首次）。
	attempts int
	result   chan sinkResult
}

type sinkResult struct {
	result contracts.UplinkResult
	err    error
}

// 分片内单 goroutine 顺序消费，confirm-before-ack 的并发上限与队列容量一致。
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
		url:      options.URL,
		token:    options.Token,
		timeout:  options.Timeout,
		attempts: NewAttemptPolicy(options.MaxAttempts, options.Attempts),
		client: &http.Client{
			Timeout: options.Timeout,
		},
		shards: shards,
		done:   make(chan struct{}),
		// confirm-before-ack 的并发上限与队列容量一致，避免无界 goroutine。
		slot:     make(chan struct{}, options.QueueSize),
		onFenced: options.OnFenced,
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
		InFlight:    s.inFlight.Load(),
		ShardQueues: shardQueues,
	}
}

// Deliver 按事件的可靠语义投递；confirm-before-ack 失败时返回错误。
func (s *HTTPSink) Deliver(ctx context.Context, event contracts.UplinkEvent) (contracts.UplinkResult, error) {
	if s == nil {
		return contracts.UplinkResult{}, fmt.Errorf("uplink http sink is not configured")
	}
	envelope, err := internalapi.BuildUplinkEnvelope(event)
	if err != nil {
		if event.Reliability == contracts.ReliabilityConfirmBeforeAck ||
			event.Reliability == contracts.ReliabilityAtLeastOnce {
			s.failed.Add(1)
			return contracts.UplinkResult{Reason: err.Error()}, err
		}
		s.dropped.Add(1)
		log.Warn().Err(err).Str("event_id", event.EventID).Msg("uplink http sink dropped event: encode failed")
		return contracts.UplinkResult{Accepted: true}, nil
	}

	attempts := s.attempts.ForEvent(event)
	if event.Reliability != contracts.ReliabilityConfirmBeforeAck &&
		event.Reliability != contracts.ReliabilityAtLeastOnce {
		return s.enqueue(envelope, attempts)
	}
	return s.deliverSync(ctx, envelope, attempts)
}

// shardFor 按 phone 选择分片；同一 phone 永远命中同一分片，从而由同一条
// goroutine 顺序处理。不同 phone 只有在哈希碰撞时才共享分片。
func (s *HTTPSink) shardFor(phone string) chan sinkJob {
	return s.shards[int(hash.FNV32(phone)%uint32(len(s.shards)))]
}

func (s *HTTPSink) enqueue(envelope internalapi.UplinkEnvelope, attempts int) (contracts.UplinkResult, error) {
	jobs := s.shardFor(envelope.Phone)
	if s.closed.Load() {
		s.dropped.Add(1)
		log.Warn().Str("event_id", envelope.EventID).Msg("uplink http sink dropped event: sink closed")
		return contracts.UplinkResult{Accepted: true}, nil
	}
	select {
	case jobs <- sinkJob{envelope: envelope, attempts: attempts}:
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

func (s *HTTPSink) deliverSync(ctx context.Context, envelope internalapi.UplinkEnvelope, attempts int) (contracts.UplinkResult, error) {
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
	case jobs <- sinkJob{envelope: envelope, attempts: attempts, result: resultCh}:
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
		resp, err := s.postWithRetry(postCtx, job.envelope, job.attempts)
		if err != nil {
			log.Warn().Err(err).Str("event_id", job.envelope.EventID).Msg("uplink http sink best-effort delivery failed")
		} else if resp.Fenced {
			s.notifyFenced(job.envelope)
		}
		return
	}

	s.inFlight.Add(1)
	defer s.inFlight.Add(-1)
	result, err := s.deliverConfirm(postCtx, job.envelope, job.attempts)
	job.result <- sinkResult{result: result, err: err}
}

func (s *HTTPSink) deliverConfirm(ctx context.Context, envelope internalapi.UplinkEnvelope, attempts int) (contracts.UplinkResult, error) {
	_, resp, err := s.postWithRetryStatus(ctx, envelope, attempts)
	if err == nil {
		return contracts.UplinkResult{
			Accepted:   true,
			Generation: resp.Generation,
			Fenced:     resp.Fenced,
		}, nil
	}
	return contracts.UplinkResult{Reason: err.Error()}, err
}

func (s *HTTPSink) postWithRetry(ctx context.Context, envelope internalapi.UplinkEnvelope, attempts int) (controlResponse, error) {
	_, resp, err := s.postWithRetryStatus(ctx, envelope, attempts)
	return resp, err
}

// postWithRetryStatus 与 postWithRetry 相同，但把最后一次 HTTP 状态码带回。
// controlResponse 是 control 上行入口返回的最小结果，confirm 与 best-effort 共用。
type controlResponse struct {
	Outcome    string `json:"outcome"`
	Generation uint64 `json:"generation"`
	Fenced     bool   `json:"fenced"`
}

func (s *HTTPSink) postWithRetryStatus(ctx context.Context, envelope internalapi.UplinkEnvelope, attempts int) (int, controlResponse, error) {
	body, err := json.Marshal(envelope)
	if err != nil {
		s.failed.Add(1)
		return 0, controlResponse{}, fmt.Errorf("marshal uplink event: %w", err)
	}

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
func (s *HTTPSink) notifyFenced(envelope internalapi.UplinkEnvelope) {
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

func (s *HTTPSink) recordFailure(envelope internalapi.UplinkEnvelope, err error, elapsed time.Duration) {
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
