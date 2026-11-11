package media

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

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
	mux.HandleFunc("/internal/media/sessions", s.handleSessions)
	mux.HandleFunc("/internal/media/sessions/", s.handleSession)
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
