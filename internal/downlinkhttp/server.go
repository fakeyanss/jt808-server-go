// Package downlinkhttp 实现管控到网关的内部下行 HTTP 契约。
//
// 网关侧只持有连接存活期内的在途记录：它不落库、不重投，权威状态在管控 outbox。
// 因此这里的记录有 TTL，未知 id 返回 404，过期 id 返回 410，明确区分
// “从未下发”与“已失效”。
package downlinkhttp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
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
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
)

const (
	// PathDownlink 是下发入口。
	PathDownlink = "/internal/v1/downlink"
	// maxDownlinkBody 限制单个下发请求体，避免异常调用方占满内存。
	maxDownlinkBody = 4 * 1024 * 1024
)

// 下行请求模式。async_callback 在 E1 明确不支持，避免半成品。
const (
	ModeAsync         = "async"
	ModeSyncAck       = "sync_ack"
	ModeAsyncCallback = "async_callback"
)

// Dependencies 是网关侧下行入口的依赖。
type Dependencies struct {
	// Token 是 control -> gateway 的共享密钥。
	Token string
	// Timeout 是 sync_ack 的等待上限。
	Timeout time.Duration
	// IDTTL 是在途记录的保留时长，也是幂等表窗口。
	IDTTL time.Duration
	// MaxInFlight 限制并发 sync_ack 等待数，超出返回 429。
	MaxInFlight int
	// Conn 用于按手机号定位本地会话。
	Conn runtime.ConnectionRegistry
	// Send 把已编码消息写入会话；serial 由网关分配或沿用调用方指定值。
	Send func(ctx context.Context, sessionID string, msg model.JT808Msg, serial uint16) error
	// Clock 便于测试控制时间；为空时使用 time.Now。
	Clock func() time.Time
}

// Server 是网关侧下行 HTTP 入口。
type Server struct {
	deps Dependencies

	mu        sync.Mutex
	byKey     map[string]*record
	byID      map[string]*record
	byRouting map[string]*record
	waiting   map[string]chan struct{}
	inflight  chan struct{}

	httpServer *http.Server
	listener   net.Listener
	startOne   sync.Once
	stopOne    sync.Once
	done       chan struct{}
}

type record struct {
	ID        string
	Key       string
	Phone     string
	MsgID     uint16
	Serial    uint16
	Status    string
	Result    *uint8
	Reason    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type downlinkRequest struct {
	Phone          string `json:"phone"`
	SessionID      string `json:"sessionId"`
	IdempotencyKey string `json:"idempotencyKey"`
	Serial         uint16 `json:"serial"`
	Mode           string `json:"mode"`
	MessageBase64  string `json:"messageBase64"`
}

type downlinkResponse struct {
	DownlinkID string `json:"downlinkId"`
	Serial     uint16 `json:"serial"`
	Status     string `json:"status"`
	Duplicated bool   `json:"duplicated"`
	Result     *uint8 `json:"result,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// NewServer 构造网关侧下行入口。
func NewServer(deps Dependencies) (*Server, error) {
	if strings.TrimSpace(deps.Token) == "" {
		return nil, fmt.Errorf("downlink http server: token is required")
	}
	if deps.Send == nil {
		return nil, fmt.Errorf("downlink http server: send function is required")
	}
	if deps.Conn == nil {
		return nil, fmt.Errorf("downlink http server: connection registry is required")
	}
	if deps.Timeout <= 0 {
		deps.Timeout = 8 * time.Second
	}
	if deps.IDTTL <= 0 {
		deps.IDTTL = 2 * time.Minute
	}
	if deps.MaxInFlight <= 0 {
		deps.MaxInFlight = 256
	}
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	return &Server{
		deps:      deps,
		byKey:     map[string]*record{},
		byID:      map[string]*record{},
		byRouting: map[string]*record{},
		waiting:   map[string]chan struct{}{},
		inflight:  make(chan struct{}, deps.MaxInFlight),
		done:      make(chan struct{}),
	}, nil
}

// Handler 返回可挂到任意 mux 的 HTTP 处理器，便于测试注入。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(PathDownlink, s.handleSubmit)
	mux.HandleFunc(PathDownlink+"/", s.handleQuery)
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
		return fmt.Errorf("downlink http server: not listening")
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

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxDownlinkBody)
	var request downlinkRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body exceeds limit")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	mode := strings.ToLower(strings.TrimSpace(request.Mode))
	if mode == "" {
		mode = ModeAsync
	}
	switch mode {
	case ModeAsync, ModeSyncAck:
	case ModeAsyncCallback:
		writeError(w, http.StatusBadRequest, "unsupported_mode", "async_callback is not supported in E1")
		return
	default:
		writeError(w, http.StatusBadRequest, "invalid_mode", fmt.Sprintf("unknown mode %q", request.Mode))
		return
	}
	if strings.TrimSpace(request.Phone) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "phone is required")
		return
	}
	if strings.TrimSpace(request.MessageBase64) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "messageBase64 is required")
		return
	}
	if len(request.IdempotencyKey) > 128 {
		writeError(w, http.StatusBadRequest, "invalid_request", "idempotencyKey must not exceed 128 characters")
		return
	}

	raw, err := base64.StdEncoding.DecodeString(request.MessageBase64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_message", "messageBase64 is not valid base64")
		return
	}
	header := &model.MsgHeader{}
	if err := header.Decode(raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_message", "cannot decode message header")
		return
	}
	body := append([]byte(nil), raw[header.Idx:]...)

	now := s.deps.Clock()
	key := strings.TrimSpace(request.IdempotencyKey)

	// 幂等命中：直接返回原记录，不重复下发。
	if key != "" {
		if existing := s.lookupByKey(key, now); existing != nil {
			writeJSON(w, http.StatusOK, s.responseFor(existing, true))
			return
		}
	}

	sessionID := strings.TrimSpace(request.SessionID)
	var session *model.Session
	if sessionID != "" {
		session, err = s.deps.Conn.Get(r.Context(), sessionID)
	} else {
		session, err = s.deps.Conn.GetByPhone(r.Context(), request.Phone)
	}
	if err != nil || session == nil {
		// 设备不在线：走 200，让控制侧保留 pending 等待设备上线。
		writeJSON(w, http.StatusOK, downlinkResponse{
			Status: contracts.DownlinkStatusOffline,
			Reason: "device is not connected to this gateway",
		})
		return
	}

	serial := request.Serial
	if serial == 0 {
		serial = session.GetNextSerialNum()
	}
	header.SerialNumber = serial
	msg := &model.MsgRawBody{Header: header, Body: body}

	response := downlinkResponse{
		DownlinkID: newDownlinkID(),
		Serial:     serial,
		Status:     contracts.DownlinkStatusAccepted,
	}

	var waiter chan struct{}
	if mode == ModeSyncAck {
		select {
		case s.inflight <- struct{}{}:
			defer func() { <-s.inflight }()
		case <-s.done:
			writeError(w, http.StatusServiceUnavailable, "closed", "server is shutting down")
			return
		default:
			writeError(w, http.StatusTooManyRequests, "too_many_inflight", "too many sync_ack requests")
			return
		}
	}

	rec := &record{
		ID:        response.DownlinkID,
		Key:       key,
		Phone:     request.Phone,
		MsgID:     header.MsgID,
		Serial:    serial,
		Status:    contracts.DownlinkStatusAccepted,
		CreatedAt: now,
		UpdatedAt: now,
	}
	waiter = s.register(rec)
	if mode != ModeSyncAck {
		waiter = nil
	}

	if err := s.deps.Send(r.Context(), session.ID, msg, serial); err != nil {
		s.update(rec.ID, func(r *record) {
			r.Status = contracts.DownlinkStatusRejected
			r.Reason = err.Error()
			r.UpdatedAt = s.deps.Clock()
		})
		s.unregisterWaiter(rec)
		log.Warn().Err(err).
			Str("downlink_id", rec.ID).
			Str("phone", rec.Phone).
			Uint16("msg_id", rec.MsgID).
			Uint16("serial", rec.Serial).
			Msg("Gateway rejected downlink before send")
		writeJSON(w, http.StatusOK, s.responseFor(rec, false))
		return
	}

	if waiter == nil {
		writeJSON(w, http.StatusAccepted, response)
		return
	}

	timer := time.NewTimer(s.deps.Timeout)
	defer timer.Stop()
	select {
	case <-waiter:
	case <-timer.C:
		s.update(rec.ID, func(r *record) {
			if r.Status == contracts.DownlinkStatusAccepted {
				r.Status = contracts.DownlinkStatusTimeout
				r.UpdatedAt = s.deps.Clock()
			}
		})
	case <-r.Context().Done():
	}
	s.mu.Lock()
	delete(s.waiting, routingKey(rec.Phone, rec.MsgID, rec.Serial))
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, s.responseFor(rec, false))
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, PathDownlink+"/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "not_found", "unknown downlink id")
		return
	}
	now := s.deps.Clock()
	s.mu.Lock()
	rec := s.byID[id]
	if rec != nil && now.Sub(rec.CreatedAt) > s.deps.IDTTL {
		delete(s.byID, id)
		if rec.Key != "" {
			delete(s.byKey, rec.Key)
		}
		delete(s.byRouting, routingKey(rec.Phone, rec.MsgID, rec.Serial))
		s.mu.Unlock()
		// 记录已失效：与“从未下发”区分。
		writeError(w, http.StatusGone, "expired", "downlink record has expired")
		return
	}
	s.mu.Unlock()
	if rec == nil {
		writeError(w, http.StatusNotFound, "not_found", "unknown downlink id")
		return
	}
	writeJSON(w, http.StatusOK, s.responseFor(rec, false))
}

// HandleReliableResult 接收会话的可靠投递结果，更新连接期内的在途记录。
// 终端应答（含 0x0001 失败结果）与重传耗尽都走这里；权威状态仍由控制侧回写。
func (s *Server) HandleReliableResult(result model.ReliableResult) {
	if s == nil {
		return
	}
	key := routingKey(result.Phone, result.MsgID, result.Serial)
	s.mu.Lock()
	rec := s.byRouting[key]
	if rec == nil {
		s.mu.Unlock()
		return
	}
	switch result.Status {
	case model.ReliableStatusAcked:
		if result.TerminalResult != nil && *result.TerminalResult != 0 {
			rec.Status = contracts.DownlinkStatusFailed
			value := *result.TerminalResult
			rec.Result = &value
		} else {
			rec.Status = contracts.DownlinkStatusAcked
			zero := uint8(0)
			rec.Result = &zero
		}
	case model.ReliableStatusFailed:
		if result.Reason == model.ReliableReasonRetransmissionExhausted {
			rec.Status = contracts.DownlinkStatusTimeout
		} else {
			rec.Status = contracts.DownlinkStatusFailed
		}
		rec.Reason = result.Reason
	}
	rec.UpdatedAt = s.deps.Clock()
	waiter := s.waiting[key]
	if waiter != nil {
		delete(s.waiting, key)
	}
	s.mu.Unlock()
	if waiter != nil {
		close(waiter)
	}
}

func (s *Server) authorized(r *http.Request) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, prefix)) == s.deps.Token
}

func (s *Server) lookupByKey(key string, now time.Time) *record {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.byKey[key]
	if rec == nil {
		return nil
	}
	if now.Sub(rec.CreatedAt) > s.deps.IDTTL {
		delete(s.byKey, key)
		delete(s.byID, rec.ID)
		delete(s.byRouting, routingKey(rec.Phone, rec.MsgID, rec.Serial))
		return nil
	}
	return rec
}

func (s *Server) register(rec *record) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[rec.ID] = rec
	if rec.Key != "" {
		s.byKey[rec.Key] = rec
	}
	s.byRouting[routingKey(rec.Phone, rec.MsgID, rec.Serial)] = rec
	waiter := make(chan struct{})
	s.waiting[routingKey(rec.Phone, rec.MsgID, rec.Serial)] = waiter
	s.cleanupLocked(s.deps.Clock())
	return waiter
}

func (s *Server) unregisterWaiter(rec *record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := routingKey(rec.Phone, rec.MsgID, rec.Serial)
	if waiter := s.waiting[key]; waiter != nil {
		close(waiter)
		delete(s.waiting, key)
	}
}

func (s *Server) update(id string, fn func(*record)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec := s.byID[id]; rec != nil {
		fn(rec)
	}
}

func (s *Server) cleanupLocked(now time.Time) {
	for id, rec := range s.byID {
		if now.Sub(rec.CreatedAt) > s.deps.IDTTL {
			delete(s.byID, id)
			if rec.Key != "" {
				delete(s.byKey, rec.Key)
			}
			delete(s.byRouting, routingKey(rec.Phone, rec.MsgID, rec.Serial))
		}
	}
}

func (s *Server) responseFor(rec *record, duplicated bool) downlinkResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return downlinkResponse{
		DownlinkID: rec.ID,
		Serial:     rec.Serial,
		Status:     rec.Status,
		Duplicated: duplicated,
		Result:     rec.Result,
		Reason:     rec.Reason,
	}
}

func routingKey(phone string, msgID, serial uint16) string {
	return fmt.Sprintf("%s|0x%04X|%d", phone, msgID, serial)
}

func newDownlinkID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("dl-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}
