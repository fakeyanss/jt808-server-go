package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// segmentIndex 是内存中的平台录像切片索引。
//
// cluster 下更合适的持久化是数据库表（recording_segments），这里只保留
// 进程内索引以保证 standalone 可用；接口与字段已按持久化模型设计。
type segmentIndex struct {
	mu    sync.RWMutex
	items map[string][]SegmentInfo
}

func newSegmentIndex() *segmentIndex {
	return &segmentIndex{items: map[string][]SegmentInfo{}}
}

func (i *segmentIndex) add(info SegmentInfo) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.items[info.Phone] = append(i.items[info.Phone], info)
	sort.Slice(i.items[info.Phone], func(a, b int) bool {
		return i.items[info.Phone][a].StartTime.Before(i.items[info.Phone][b].StartTime)
	})
}

func (i *segmentIndex) query(phone string, channel uint8, start, end time.Time) []SegmentInfo {
	i.mu.RLock()
	defer i.mu.RUnlock()
	var out []SegmentInfo
	for _, item := range i.items[phone] {
		if item.Channel != channel {
			continue
		}
		if !item.EndTime.After(start) || !item.StartTime.Before(end) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (i *segmentIndex) find(phone, id string) (SegmentInfo, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	for _, item := range i.items[phone] {
		if segmentID(item) == id {
			return item, true
		}
	}
	return SegmentInfo{}, false
}

// segmentID 是切片对外的稳定标识；ObjectKey 可能随部署变化，因此用终端与起点。
func segmentID(info SegmentInfo) string {
	return fmt.Sprintf("%s_%d_%d", info.Phone, info.Channel, info.StartTime.UnixMilli())
}

// onSegment 把完成的切片写入本地索引与持久化仓储，再交给外部 Sink。
func (s *Server) onSegment(ctx context.Context, info SegmentInfo) error {
	s.segments.add(info)
	if s.cfg.SegmentRepository != nil {
		if err := s.cfg.SegmentRepository.Save(ctx, toRecordingSegment(info)); err != nil {
			log.Warn().Err(err).Str("phone", info.Phone).Uint8("channel", info.Channel).
				Str("objectKey", info.ObjectKey).Msg("Fail to persist media segment index")
		}
	}
	if s.cfg.SegmentSink != nil {
		return s.cfg.SegmentSink.OnSegment(ctx, info)
	}
	return nil
}

// toRecordingSegment 把录制侧切片信息转换成持久化索引行。
func toRecordingSegment(info SegmentInfo) *repository.RecordingSegment {
	keyframes, err := json.Marshal(info.Keyframes)
	if err != nil {
		keyframes = []byte("[]")
	}
	state := repository.RecordingSegmentStateLocal
	if info.State == "committed" {
		state = repository.RecordingSegmentStateCommitted
	}
	return &repository.RecordingSegment{
		ID:        segmentID(info),
		Phone:     info.Phone,
		Channel:   info.Channel,
		Kind:      string(info.Kind),
		ObjectKey: info.ObjectKey,
		LocalPath: info.LocalPath,
		StartAt:   info.StartTime,
		EndAt:     info.EndTime,
		Size:      info.Size,
		Keyframes: keyframes,
		State:     state,
		CreatedAt: info.StartTime,
	}
}

// fromRecordingSegment 把持久化索引还原成切片信息。
func fromRecordingSegment(segment *repository.RecordingSegment) SegmentInfo {
	var keyframes []SegmentKeyframe
	if len(segment.Keyframes) > 0 {
		_ = json.Unmarshal(segment.Keyframes, &keyframes)
	}
	return SegmentInfo{
		Phone:     segment.Phone,
		Channel:   segment.Channel,
		Kind:      StreamKind(segment.Kind),
		ObjectKey: segment.ObjectKey,
		LocalPath: segment.LocalPath,
		StartTime: segment.StartAt,
		EndTime:   segment.EndAt,
		Duration:  segment.Duration(),
		Size:      segment.Size,
		Keyframes: keyframes,
		State:     segment.State,
	}
}

// querySegments 优先按持久化索引查询；仓储不可用时回落到进程内索引。
func (s *Server) querySegments(ctx context.Context, phone string, channel uint8, start, end time.Time) []SegmentInfo {
	if s.cfg.SegmentRepository != nil {
		rows, err := s.cfg.SegmentRepository.ListByRange(ctx, phone, channel, start, end)
		if err == nil {
			out := make([]SegmentInfo, 0, len(rows))
			for _, row := range rows {
				out = append(out, fromRecordingSegment(row))
			}
			return out
		}
		log.Warn().Err(err).Str("phone", phone).Uint8("channel", channel).
			Msg("Fail to query media segment index, fallback to in-memory index")
	}
	return s.segments.query(phone, channel, start, end)
}

// CreatePlaybackSession 申请播放会话，返回终端应推流的端点与播放地址。
func (s *Server) CreatePlaybackSession(ctx context.Context, request contracts.PlaybackSessionRequest) (contracts.PlaybackSession, error) {
	kind, err := NormalizeStreamKind(StreamKind(request.Kind))
	if err != nil {
		return contracts.PlaybackSession{}, err
	}
	info, err := s.Acquire(SessionRequest{
		Phone:   request.Phone,
		Channel: request.Channel,
		Kind:    kind,
		TTL:     request.TTL,
	})
	if err != nil {
		var conflict *SessionConflictError
		if errors.As(err, &conflict) {
			existing, _, _ := s.Session(conflict.ExistingSessionID)
			return contracts.PlaybackSession{}, &contracts.PlaybackSessionConflictError{
				Existing: toContractSession(existing),
			}
		}
		return contracts.PlaybackSession{}, err
	}
	return toContractSession(info), nil
}

// ReleasePlaybackSession 释放播放会话。
func (s *Server) ReleasePlaybackSession(_ context.Context, sessionID string) error {
	return s.Release(sessionID)
}

// PlaybackSession 返回会话快照；不存在时 found=false。
func (s *Server) PlaybackSession(_ context.Context, sessionID string) (contracts.PlaybackSession, bool) {
	info, _, ok := s.Session(sessionID)
	if !ok {
		return contracts.PlaybackSession{}, false
	}
	return toContractSession(info), true
}

func toContractSession(info SessionInfo) contracts.PlaybackSession {
	out := contracts.PlaybackSession{
		ID:          info.ID,
		Phone:       info.Phone,
		Channel:     info.Channel,
		Kind:        string(info.Kind),
		ExpiresAt:   info.ExpiresAt,
		FLVPath:     info.FLVPath,
		Output:      info.Output,
		PlaybackURL: info.PlaybackURL,
		Endpoint: contracts.MediaEndpoint{
			IP:      info.ServerIP,
			TCPPort: info.TCPPort,
			UDPPort: info.UDPPort,
		},
	}
	for _, endpoint := range info.Endpoints {
		out.Endpoints = append(out.Endpoints, contracts.PlaybackEndpoint{
			Protocol: endpoint.Protocol,
			URL:      endpoint.URL,
		})
	}
	return out
}

// RecordingCoverage 返回平台录像在请求区间内的覆盖情况。
func (s *Server) RecordingCoverage(ctx context.Context, phone string, channel uint8, start, end time.Time) (contracts.RecordingCoverage, error) {
	if !start.Before(end) {
		return contracts.RecordingCoverage{}, errors.New("start must be before end")
	}
	result := contracts.RecordingCoverage{Requested: contracts.TimeRange{Start: start, End: end}}
	items := s.querySegments(ctx, phone, channel, start, end)
	for _, item := range items {
		result.Segments = append(result.Segments, contracts.RecordingSegmentInfo{
			ID:        segmentID(item),
			Phone:     item.Phone,
			Channel:   item.Channel,
			Start:     item.StartTime,
			End:       item.EndTime,
			Size:      item.Size,
			ObjectKey: item.ObjectKey,
		})
		from := item.StartTime
		if from.Before(start) {
			from = start
		}
		to := item.EndTime
		if to.After(end) {
			to = end
		}
		if !from.Before(to) {
			continue
		}
		if n := len(result.Available); n > 0 && !from.After(result.Available[n-1].End) {
			if to.After(result.Available[n-1].End) {
				result.Available[n-1].End = to
			}
			continue
		}
		result.Available = append(result.Available, contracts.TimeRange{Start: from, End: to})
	}
	cursor := start
	for _, available := range result.Available {
		if cursor.Before(available.Start) {
			result.Missing = append(result.Missing, contracts.TimeRange{Start: cursor, End: available.Start})
		}
		if available.End.After(cursor) {
			cursor = available.End
		}
	}
	if cursor.Before(end) {
		result.Missing = append(result.Missing, contracts.TimeRange{Start: cursor, End: end})
	}
	result.Playable = len(result.Missing) == 0
	return result, nil
}

// OpenRecordingSegment 打开平台录像切片，供 HTTP Range 读取。
func (s *Server) OpenRecordingSegment(ctx context.Context, phone, segmentIDValue string, offset, length int64) (io.ReadCloser, int64, error) {
	var (
		info SegmentInfo
		ok   bool
	)
	if s.cfg.SegmentRepository != nil {
		row, err := s.cfg.SegmentRepository.Get(ctx, phone, segmentIDValue)
		switch {
		case err == nil:
			info, ok = fromRecordingSegment(row), true
		case !errors.Is(err, repository.ErrRecordingSegmentNotFound):
			log.Warn().Err(err).Str("phone", phone).Str("segment", segmentIDValue).
				Msg("Fail to load media segment index")
		}
	}
	if !ok {
		info, ok = s.segments.find(phone, segmentIDValue)
	}
	if !ok {
		return nil, 0, contracts.ErrRecordingNotFound
	}
	size := info.Size
	if size <= 0 {
		size = 0
	}
	if offset < 0 || offset > size {
		return nil, 0, contracts.ErrRangeNotSatisfiable
	}
	if length <= 0 || offset+length > size {
		length = size - offset
	}
	if info.ObjectKey != "" && s.cfg.Blob != nil {
		reader, err := s.cfg.Blob.OpenRange(ctx, info.ObjectKey, offset, length)
		if err != nil {
			return nil, 0, err
		}
		return reader, size, nil
	}
	if info.LocalPath == "" {
		return nil, 0, contracts.ErrRecordingNotFound
	}
	file, err := os.Open(info.LocalPath)
	if err != nil {
		return nil, 0, err
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, 0, err
	}
	return struct {
		io.Reader
		io.Closer
	}{Reader: io.LimitReader(file, length), Closer: file}, size, nil
}

// cleanupLocalSegment 删除已成功上传的本地切片，避免磁盘长期占用。
func cleanupLocalSegment(path string) {
	if path == "" {
		return
	}
	if filepath.Ext(path) != ".ts" {
		return
	}
	_ = os.Remove(path)
}
