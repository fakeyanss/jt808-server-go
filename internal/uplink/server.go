package uplink

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

const (
	// PathUplink 是管控侧上行入口。
	PathUplink = "/internal/v1/uplink"
	// PathDeviceLookup 是网关只读查询设备档案的入口。
	PathDeviceLookup = "/internal/v1/device/lookup"
)

// EventConsumer 处理某一类上行事件。
//
// 返回 error 表示处理失败：confirm-before-ack 事件会回 503，让网关不应答终端；
// best-effort 事件回 202 并记录，因为网关已经应答过终端。
type EventConsumer interface {
	ConsumeEvent(ctx context.Context, event contracts.UplinkEvent) (contracts.UplinkResult, error)
}

// StatusError 是带 HTTP 状态码的业务错误，例如 409 unknown_device。
type StatusError struct {
	Status  int
	Code    string
	Message string
}

func (e *StatusError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

// ServerOptions 是管控侧上行入口的构造参数。
type ServerOptions struct {
	// Token 是 gateway -> control 的共享密钥。
	Token string
	// DedupTTL 是 eventId 去重窗口，必须覆盖网关超时与重试窗口。
	DedupTTL time.Duration
	// DedupMaxEntries 限制去重表规模。
	DedupMaxEntries int
	// MaxRequestBytes 限制 HTTP 请求体（含 base64 膨胀）；超出返回 413。
	MaxRequestBytes int64
	// MaxDecodedBodyBytes 在解码后二次校验原始消息体；超出返回 413。
	MaxDecodedBodyBytes int64
}

// Server 是管控侧上行 HTTP 入口。
type Server struct {
	opts ServerOptions

	mu              sync.RWMutex
	consumers       map[uint16]EventConsumer
	defaultConsumer EventConsumer
	replyHandler    contracts.TerminalReplyHandler
	dedup           *dedupCache
	deviceLookup    func(ctx context.Context, phone string) (*model.Device, error)

	httpServer *http.Server
	listener   net.Listener
	startOne   sync.Once
	stopOne    sync.Once
	done       chan struct{}
}

// NewServer 构造管控侧上行入口。
func NewServer(options ServerOptions) (*Server, error) {
	if strings.TrimSpace(options.Token) == "" {
		return nil, fmt.Errorf("uplink server: token is required")
	}
	if options.DedupTTL <= 0 {
		options.DedupTTL = 10 * time.Minute
	}
	if options.DedupMaxEntries <= 0 {
		options.DedupMaxEntries = 100000
	}
	if options.MaxRequestBytes <= 0 {
		options.MaxRequestBytes = 12 * 1024 * 1024
	}
	if options.MaxDecodedBodyBytes <= 0 {
		options.MaxDecodedBodyBytes = 8 * 1024 * 1024
	}
	return &Server{
		opts:      options,
		consumers: map[uint16]EventConsumer{},
		dedup:     newDedupCache(options.DedupTTL, options.DedupMaxEntries),
		done:      make(chan struct{}),
	}, nil
}

// RegisterConsumer 按消息 ID 注册处理方；同一 ID 重复注册会覆盖。
func (s *Server) RegisterConsumer(msgID uint16, consumer EventConsumer) {
	if s == nil || consumer == nil {
		return
	}
	s.mu.Lock()
	s.consumers[msgID] = consumer
	s.mu.Unlock()
}

// RegisterDefaultConsumer 注册未匹配到具体消息 ID 时的兜底处理方。
func (s *Server) RegisterDefaultConsumer(consumer EventConsumer) {
	if s == nil || consumer == nil {
		return
	}
	s.mu.Lock()
	s.defaultConsumer = consumer
	s.mu.Unlock()
}

// SetTerminalReplyHandler 设置 0x0001 应答的统一处理入口。
func (s *Server) SetTerminalReplyHandler(handler contracts.TerminalReplyHandler) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.replyHandler = handler
	s.mu.Unlock()
}

// SetDeviceLookup 设置网关只读查询设备档案的回调。
func (s *Server) SetDeviceLookup(lookup func(ctx context.Context, phone string) (*model.Device, error)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.deviceLookup = lookup
	s.mu.Unlock()
}

// ValidateCoverage 校验 confirm-before-ack 覆盖：所有 at_least_once 类别，
// 以及被强制 confirm 的 0x0001、0x0801，都必须已经注册处理方。
// 缺失时返回一次性列出的错误；E1 不提供跳过开关。
func (s *Server) ValidateCoverage(policy Policy) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var missing []string
	seen := map[string]bool{}
	add := func(item string) {
		if !seen[item] {
			seen[item] = true
			missing = append(missing, item)
		}
	}
	if s.replyHandler == nil {
		add("0x0001(terminal reply)")
	}
	if s.consumers[0x0801] == nil {
		add("0x0801(forced confirm-before-ack)")
	}
	if policy.Default == AtLeastOnce && s.defaultConsumer == nil {
		add("default(category)")
	}
	if policy.Location == AtLeastOnce && s.consumers[0x0200] == nil {
		add("0x0200(location)")
	}
	if policy.BatchLocation == AtLeastOnce && s.consumers[0x0704] == nil {
		add("0x0704(batch_location)")
	}
	if policy.Alarm == AtLeastOnce && s.consumers[0x0200] == nil {
		add("0x0200(alarm)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("confirm-before-ack consumers are missing: %s", strings.Join(missing, ", "))
	}
	return nil
}

// Handler 返回可挂到任意 mux 的 HTTP 处理器，便于测试注入。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(PathUplink, s.handle)
	mux.HandleFunc(PathDeviceLookup, s.handleDeviceLookup)
	return mux
}

// Listen 绑定监听地址；尚未开始接收请求。
func (s *Server) Listen(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.listener = ln
	s.httpServer = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	return nil
}

// Addr 返回实际监听地址；未监听时返回空串。
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Start 开始接收请求，直到 Stop 或监听失败。
func (s *Server) Start() error {
	if s.httpServer == nil || s.listener == nil {
		return fmt.Errorf("uplink server: not listening")
	}
	var serveErr error
	s.startOne.Do(func() {
		serveErr = s.httpServer.Serve(s.listener)
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		close(s.done)
	})
	return serveErr
}

// Stop 停止监听并等待在途请求收尾。
func (s *Server) Stop(ctx context.Context) error {
	var stopErr error
	s.stopOne.Do(func() {
		if s.httpServer != nil {
			stopErr = s.httpServer.Shutdown(ctx)
		}
	})
	return stopErr
}

// Done 在 Serve 退出后关闭。
func (s *Server) Done() <-chan struct{} { return s.done }

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.opts.MaxRequestBytes)
	var envelope HTTPEnvelope
	if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body exceeds limit")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(envelope.EventID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "eventId is required")
		return
	}

	now := time.Now()
	if s.dedup.Seen(envelope.EventID, now) {
		writeJSON(w, http.StatusOK, map[string]any{"outcome": "duplicate"})
		return
	}

	confirm := strings.EqualFold(envelope.Reliability, string(contracts.ReliabilityConfirmBeforeAck)) ||
		strings.EqualFold(envelope.Reliability, string(AtLeastOnce))

	// 0x0001 终端应答是控制侧拿 ack 的主路径，必须走统一入口。
	if envelope.Reply != nil {
		s.mu.RLock()
		handler := s.replyHandler
		s.mu.RUnlock()
		if handler == nil {
			writeError(w, http.StatusServiceUnavailable, "no_consumer", "terminal reply handler is not registered")
			return
		}
		outcome, err := handler.HandleTerminalReply(r.Context(), *envelope.Reply)
		if err != nil {
			log.Error().Err(err).Str("event_id", envelope.EventID).Msg("Failed to handle terminal reply")
			writeError(w, http.StatusServiceUnavailable, "handle_failed", err.Error())
			return
		}
		s.dedup.Add(envelope.EventID, now)
		writeJSON(w, http.StatusOK, map[string]any{"outcome": string(outcome)})
		return
	}

	s.mu.RLock()
	consumer := s.consumers[envelope.MsgID]
	if consumer == nil {
		consumer = s.defaultConsumer
	}
	s.mu.RUnlock()
	if consumer == nil {
		if confirm {
			// confirm-before-ack 找不到处理方时绝不回 2xx，否则终端会停止重传。
			writeError(w, http.StatusServiceUnavailable, "no_consumer",
				fmt.Sprintf("no consumer registered for msg 0x%04X", envelope.MsgID))
			return
		}
		log.Warn().
			Str("event_id", envelope.EventID).
			Uint16("msg_id", envelope.MsgID).
			Msg("Uplink event has no consumer; dropped after gateway ack")
		s.dedup.Add(envelope.EventID, now)
		writeJSON(w, http.StatusAccepted, map[string]any{"outcome": "unhandled"})
		return
	}

	event, err := envelopeToEvent(envelope)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}
	if s.opts.MaxDecodedBodyBytes > 0 && int64(len(event.Body)) > s.opts.MaxDecodedBodyBytes {
		log.Error().
			Str("event_id", envelope.EventID).
			Str("phone", envelope.Phone).
			Uint16("msg_id", envelope.MsgID).
			Int("body_bytes", len(event.Body)).
			Int64("limit_bytes", s.opts.MaxDecodedBodyBytes).
			Msg("Decoded uplink body exceeds limit")
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "decoded body exceeds limit")
		return
	}
	result, err := consumer.ConsumeEvent(r.Context(), event)
	if err != nil {
		var statusErr *StatusError
		if errors.As(err, &statusErr) {
			log.Warn().Err(err).Str("event_id", envelope.EventID).Int("status", statusErr.Status).
				Msg("Uplink event rejected with explicit status")
			writeError(w, statusErr.Status, statusErr.Code, statusErr.Message)
			return
		}
		if confirm {
			log.Error().Err(err).Str("event_id", envelope.EventID).Msg("Failed to consume confirm-before-ack event")
			writeError(w, http.StatusServiceUnavailable, "handle_failed", err.Error())
			return
		}
		log.Warn().Err(err).Str("event_id", envelope.EventID).Msg("Best-effort uplink event was dropped")
		s.dedup.Add(envelope.EventID, now)
		writeJSON(w, http.StatusAccepted, map[string]any{"outcome": "failed", "reason": err.Error()})
		return
	}
	s.dedup.Add(envelope.EventID, now)
	body := map[string]any{"outcome": "accepted"}
	if result.Reason != "" {
		body["reason"] = result.Reason
	}
	if len(result.Reply) > 0 {
		body["reply"] = base64.StdEncoding.EncodeToString(result.Reply)
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) handleDeviceLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token")
		return
	}
	s.mu.RLock()
	lookup := s.deviceLookup
	s.mu.RUnlock()
	if lookup == nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "device lookup is not configured")
		return
	}
	var request struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	device, err := lookup(r.Context(), strings.TrimSpace(request.Phone))
	if err != nil {
		if errors.Is(err, repository.ErrDeviceNotFound) {
			writeError(w, http.StatusNotFound, "device_not_found", "device is not registered")
			return
		}
		writeError(w, http.StatusServiceUnavailable, "lookup_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"phone":    device.Phone,
		"id":       device.ID,
		"plate":    device.Plate,
		"authCode": device.AuthCode,
	})
}

func (s *Server) authorized(r *http.Request) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, prefix)) == s.opts.Token
}

// envelopeToEvent 把 HTTP 信封还原成契约事件。
// Payload 保留为 json.RawMessage，由具体 consumer 决定如何解码。
func envelopeToEvent(envelope HTTPEnvelope) (contracts.UplinkEvent, error) {
	event := contracts.UplinkEvent{
		EventID:             envelope.EventID,
		Phone:               envelope.Phone,
		MsgID:               envelope.MsgID,
		RawMsgID:            envelope.RawMsgID,
		Serial:              envelope.Serial,
		GatewayID:           envelope.GatewayID,
		SessionID:           envelope.SessionID,
		Transport:           envelope.Transport,
		Reliability:         contracts.Reliability(envelope.Reliability),
		ProtocolVersion:     envelope.ProtocolVersion,
		HeaderAttr:          envelope.HeaderAttr,
		FragmentReassembled: envelope.FragmentReassembled,
		FragmentTotal:       envelope.FragmentTotal,
		Payload:             envelope.Payload,
		PayloadRaw:          nil,
		Meta:                envelope.Meta,
		Reply:               envelope.Reply,
	}
	if envelope.RawMsgID == 0 {
		event.RawMsgID = envelope.MsgID
	}
	if envelope.OccurredAtMS > 0 {
		event.OccurredAt = time.UnixMilli(envelope.OccurredAtMS)
	}
	if envelope.BodyBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(envelope.BodyBase64)
		if err != nil {
			return contracts.UplinkEvent{}, fmt.Errorf("bodyBase64 is not valid base64")
		}
		event.Body = decoded
	}
	if envelope.PayloadBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(envelope.PayloadBase64)
		if err != nil {
			return contracts.UplinkEvent{}, fmt.Errorf("payloadBase64 is not valid base64")
		}
		event.PayloadRaw = decoded
	}
	return event, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

// dedupCache 是 eventId 的有界 TTL 去重表。
// 超过上限时先清理过期项；仍满则淘汰任意项，保证内存有界并记录告警。
type dedupCache struct {
	mu        sync.Mutex
	ttl       time.Duration
	max       int
	entries   map[string]time.Time
	lastSweep time.Time
	inserts   int
}

func newDedupCache(ttl time.Duration, max int) *dedupCache {
	return &dedupCache{ttl: ttl, max: max, entries: map[string]time.Time{}}
}

// Seen 判断事件是否已在去重窗口内处理过；过期项会被顺带删除。
// 与 Add 分开是为了 confirm-before-ack：只有处理成功才记录，
// 失败回 503 时不能落表，否则网关重试会被误判为 duplicate 而被应答。
func (c *dedupCache) Seen(key string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	seenAt, ok := c.entries[key]
	if !ok {
		return false
	}
	if now.Sub(seenAt) <= c.ttl {
		return true
	}
	delete(c.entries, key)
	return false
}

// Add 记录已成功处理的事件。
func (c *dedupCache) Add(key string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.max {
		c.sweepLocked(now)
		for len(c.entries) >= c.max {
			for existing := range c.entries {
				delete(c.entries, existing)
				break
			}
		}
		log.Warn().Int("max_entries", c.max).Msg("Uplink dedup cache is full; evicted an entry before expiry")
	}
	c.entries[key] = now
	c.inserts++
	if c.inserts%1024 == 0 {
		c.sweepLocked(now)
	}
}

func (c *dedupCache) sweepLocked(now time.Time) {
	c.lastSweep = now
	for key, seenAt := range c.entries {
		if now.Sub(seenAt) > c.ttl {
			delete(c.entries, key)
		}
	}
}
