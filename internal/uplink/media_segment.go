package uplink

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// PathMediaSegment 是 media 节点上报切片索引的出站入口。
const PathMediaSegment = "/internal/v1/media/segments"

// mediaSegmentReport 是 media 上报的最小索引载荷。
//
// control 不依赖该载荷：media/segments/ 下的对象 key 自带完整元数据，
// 通知丢失时由后台扫描补齐。这里只做幂等 upsert。
type mediaSegmentReport struct {
	Phone     string          `json:"phone"`
	Channel   uint8           `json:"channel"`
	Kind      string          `json:"kind"`
	ObjectKey string          `json:"objectKey"`
	LocalPath string          `json:"localPath"`
	StartTime time.Time       `json:"startTime"`
	EndTime   time.Time       `json:"endTime"`
	Size      int64           `json:"size"`
	Keyframes json.RawMessage `json:"keyframes"`
	State     string          `json:"state"`
}

func (s *Server) handleMediaSegment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token")
		return
	}
	s.mu.RLock()
	segments := s.segments
	s.mu.RUnlock()
	if segments == nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "recording segment index is not configured")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var report mediaSegmentReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	segment, err := report.toRepository()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := segments.Save(r.Context(), segment); err != nil {
		log.Error().Err(err).Str("phone", segment.Phone).Str("key", segment.ObjectKey).
			Msg("Fail to persist reported media segment index")
		writeError(w, http.StatusServiceUnavailable, "persist_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"outcome": "accepted"})
}

func (r mediaSegmentReport) toRepository() (*repository.RecordingSegment, error) {
	phone := strings.TrimSpace(r.Phone)
	if phone == "" {
		return nil, errors.New("phone is required")
	}
	if r.StartTime.IsZero() || r.EndTime.IsZero() || !r.StartTime.Before(r.EndTime) {
		return nil, errors.New("startTime must be before endTime")
	}
	if strings.TrimSpace(r.ObjectKey) == "" && strings.TrimSpace(r.LocalPath) == "" {
		return nil, errors.New("objectKey or localPath is required")
	}
	keyframes := r.Keyframes
	if len(keyframes) == 0 {
		keyframes = []byte("[]")
	}
	state := repository.RecordingSegmentStateLocal
	if r.State == repository.RecordingSegmentStateCommitted {
		state = repository.RecordingSegmentStateCommitted
	}
	return &repository.RecordingSegment{
		ID:        fmt.Sprintf("%s_%d_%d", phone, r.Channel, r.StartTime.UnixMilli()),
		Phone:     phone,
		Channel:   r.Channel,
		Kind:      strings.TrimSpace(r.Kind),
		ObjectKey: strings.TrimSpace(r.ObjectKey),
		LocalPath: strings.TrimSpace(r.LocalPath),
		StartAt:   r.StartTime,
		EndAt:     r.EndTime,
		Size:      r.Size,
		Keyframes: keyframes,
		State:     state,
		CreatedAt: r.StartTime,
	}, nil
}
