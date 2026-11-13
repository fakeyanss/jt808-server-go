package media

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

// ListenHTTP 启动媒体节点内部 HTTP 服务：会话管理与 FLV 播放。
func (s *Server) ListenHTTP(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/internal/media/status", s.handleStatus)
	mux.HandleFunc("/internal/media/sessions", s.handleSessions)
	mux.HandleFunc("/internal/media/sessions/", s.handleSession)
	mux.HandleFunc("/internal/media/recordings/coverage", s.handleCoverage)
	mux.HandleFunc("/internal/media/recordings/segments/", s.handleSegmentContent)
	if handler := s.playbackHandler(); handler != nil {
		mux.Handle("/media/", handler)
	}
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.httpLn = listener
	s.httpSrv = server
	routines.GoSafe(func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("Media HTTP server stopped")
		}
	})
	log.Info().Str("addr", listener.Addr().String()).Msg("Media HTTP server listening")
	return nil
}

// NodeID 返回集群内唯一的节点标识：显式配置优先，否则按播出地址推导。
func (s *Server) NodeID() string {
	if s == nil {
		return ""
	}
	if id := strings.TrimSpace(s.cfg.NodeID); id != "" {
		return id
	}
	host := s.cfg.AdvertiseIP
	if host == "" {
		host = "127.0.0.1"
	}
	port := s.udpPort
	if port == 0 {
		port = s.tcpPort
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// Status 是媒体节点当前的会话与并发额度快照，供管控挑选节点。
type Status struct {
	NodeID         string        `json:"nodeId"`
	Sessions       []SessionInfo `json:"sessions"`
	Active         int           `json:"active"`
	MaxConcurrent  int           `json:"maxConcurrent"`
	Available      int           `json:"available"`
	MaxTCPConns    int           `json:"maxTCPConns"`
	ActiveTCPConns int           `json:"activeTcpConns"`
}

// Status 返回当前会话列表与剩余并发额度。
func (s *Server) Status() Status {
	sessions := s.Sessions()
	active := len(sessions)
	available := s.cfg.MaxConcurrent - active
	if available < 0 {
		available = 0
	}
	s.mu.Lock()
	activeTCP := len(s.conns)
	s.mu.Unlock()
	return Status{
		NodeID:         s.NodeID(),
		Sessions:       sessions,
		Active:         active,
		MaxConcurrent:  s.cfg.MaxConcurrent,
		Available:      available,
		MaxTCPConns:    s.cfg.MaxTCPConns,
		ActiveTCPConns: activeTCP,
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, s.Status())
}

func (s *Server) handleCoverage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	phone := strings.TrimSpace(query.Get("phone"))
	if phone == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Code: "invalid_request", Message: "phone is required"})
		return
	}
	channel, err := strconv.ParseUint(query.Get("channel"), 10, 8)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Code: "invalid_request", Message: "channel must be 0-255"})
		return
	}
	start, err := time.Parse(time.RFC3339Nano, query.Get("start"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Code: "invalid_request", Message: "start must be RFC3339"})
		return
	}
	end, err := time.Parse(time.RFC3339Nano, query.Get("end"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Code: "invalid_request", Message: "end must be RFC3339"})
		return
	}
	coverage, err := s.RecordingCoverage(r.Context(), phone, uint8(channel), start, end)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Code: "invalid_request", Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, coverage)
}

func (s *Server) handleSegmentContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rest := strings.TrimPrefix(path.Clean(r.URL.Path), "/internal/media/recordings/segments/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Code: "invalid_request", Message: "phone and segment id are required"})
		return
	}
	phone, segmentID := parts[0], parts[1]
	query := r.URL.Query()
	offset, _ := strconv.ParseInt(query.Get("offset"), 10, 64)
	length, _ := strconv.ParseInt(query.Get("length"), 10, 64)
	reader, size, err := s.OpenRecordingSegment(r.Context(), phone, segmentID, offset, length)
	if err != nil {
		if errors.Is(err, contracts.ErrRecordingNotFound) {
			writeJSON(w, http.StatusNotFound, errorBody{Code: "recording_not_found", Message: err.Error()})
			return
		}
		if errors.Is(err, contracts.ErrRangeNotSatisfiable) {
			writeJSON(w, http.StatusRequestedRangeNotSatisfiable, errorBody{Code: "range_not_satisfiable", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody{Code: "internal_error", Message: err.Error()})
		return
	}
	defer reader.Close()
	if length <= 0 || offset+length > size {
		length = size - offset
	}
	if length < 0 {
		length = 0
	}
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("X-Segment-Size", strconv.FormatInt(size, 10))
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, io.LimitReader(reader, length))
	}
}

// HTTPAddr 返回媒体 HTTP 实际监听地址。
func (s *Server) HTTPAddr() string {
	if s.httpLn == nil {
		return ""
	}
	return s.httpLn.Addr().String()
}

type createSessionRequest struct {
	Phone      string     `json:"phone"`
	Channel    uint8      `json:"channel"`
	Kind       StreamKind `json:"kind"`
	TTLSeconds int        `json:"ttlSeconds"`
}

type errorBody struct {
	Code     string       `json:"code"`
	Message  string       `json:"message"`
	Existing *sessionView `json:"existing,omitempty"`
}

type sessionView struct {
	SessionID string     `json:"sessionId"`
	Kind      StreamKind `json:"kind"`
	ExpiresAt time.Time  `json:"expiresAt"`
	FLVPath   string     `json:"flvPath"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.Sessions())
	case http.MethodPost:
		var req createSessionRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Code: "invalid_request", Message: err.Error()})
			return
		}
		info, err := s.Acquire(SessionRequest{
			Phone:   req.Phone,
			Channel: req.Channel,
			Kind:    req.Kind,
			TTL:     time.Duration(req.TTLSeconds) * time.Second,
		})
		if err != nil {
			var conflict *SessionConflictError
			if errors.As(err, &conflict) {
				writeJSON(w, http.StatusConflict, errorBody{
					Code:    conflict.Code(),
					Message: conflict.Error(),
					Existing: &sessionView{
						SessionID: conflict.ExistingSessionID,
						Kind:      conflict.ExistingKind,
						ExpiresAt: conflict.ExistingExpiresAt,
						FLVPath:   "/media/" + conflict.ExistingSessionID + ".flv",
					},
				})
				return
			}
			writeJSON(w, http.StatusBadRequest, errorBody{Code: "invalid_request", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, info)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", "DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(path.Clean(r.URL.Path), "/internal/media/sessions/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Code: "invalid_request", Message: "session id is required"})
		return
	}
	if err := s.Release(id); err != nil {
		var notFound *SessionNotFoundError
		if errors.As(err, &notFound) {
			writeJSON(w, http.StatusNotFound, errorBody{Code: notFound.Code(), Message: notFound.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody{Code: "internal_error", Message: err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
