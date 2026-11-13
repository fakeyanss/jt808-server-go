package uplink

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

// PathAttachmentProgress 是附件节点上报进度、查询恢复点的出站入口。
const PathAttachmentProgress = "/internal/v1/attachment/progress"

// AttachmentProgressStore 是 control 侧附件进度的落库与查询能力。
type AttachmentProgressStore interface {
	Save(ctx context.Context, progress contracts.AttachmentProgress) error
	Load(ctx context.Context, key contracts.AttachmentProgressKey) (*contracts.AttachmentProgress, error)
}

// handleAttachmentProgress 同时承担两条出站请求：
//   - POST：附件节点把进度快照推给 control（幂等 upsert）；
//   - GET ：附件节点重连/重启后按业务键取回恢复点。
func (s *Server) handleAttachmentProgress(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token")
		return
	}
	s.mu.RLock()
	store := s.attachments
	s.mu.RUnlock()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "attachment progress store is not configured")
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.saveAttachmentProgress(w, r, store)
	case http.MethodGet:
		s.loadAttachmentProgress(w, r, store)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST or GET")
	}
}

func (s *Server) saveAttachmentProgress(w http.ResponseWriter, r *http.Request, store AttachmentProgressStore) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	var progress contracts.AttachmentProgress
	if err := json.NewDecoder(r.Body).Decode(&progress); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := store.Save(r.Context(), progress); err != nil {
		log.Error().Err(err).Str("phone", progress.Phone).Str("file", progress.FileName).
			Msg("Fail to persist attachment progress")
		writeError(w, http.StatusBadRequest, "persist_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"outcome": "accepted"})
}

func (s *Server) loadAttachmentProgress(w http.ResponseWriter, r *http.Request, store AttachmentProgressStore) {
	query := r.URL.Query()
	key := contracts.AttachmentProgressKey{
		Source:   strings.TrimSpace(query.Get("source")),
		Phone:    strings.TrimSpace(query.Get("phone")),
		AlarmNo:  strings.TrimSpace(query.Get("alarmNo")),
		FileName: strings.TrimSpace(query.Get("fileName")),
	}
	if key.Phone == "" || key.FileName == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "phone and fileName are required")
		return
	}
	progress, err := store.Load(r.Context(), key)
	if err != nil {
		if err == contracts.ErrAttachmentProgressNotFound {
			writeError(w, http.StatusNotFound, "not_found", "attachment progress is not found")
			return
		}
		writeError(w, http.StatusServiceUnavailable, "load_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, progress)
}
