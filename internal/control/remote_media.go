package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

// 管控调用媒体节点内部 HTTP 的默认超时。
const (
	defaultMediaNodeTimeout = 3 * time.Second
	defaultMediaLeaseTTL    = 10 * time.Minute
)

// MediaNode 是一个可被管控调度的媒体节点。
type MediaNode struct {
	Name string
	URL  string
}

// RemoteMediaOptions 是远程媒体调度器的构造参数。
type RemoteMediaOptions struct {
	// Nodes 是管控可选的媒体节点清单，顺序即优先级的兜底顺序。
	Nodes []MediaNode
	// Timeout 是单次内部 HTTP 调用超时；<=0 时使用默认值。
	Timeout time.Duration
	// DefaultTTL 是播放会话的默认存活时长；<=0 时使用 10 分钟。
	DefaultTTL time.Duration
}

// RemoteMedia 是跨进程的媒体调度器：
//
//   - 实现 contracts.MediaAllocator：Allocate 在媒体节点上创建播放会话；
//   - 实现 contracts.PlaybackLibrary：播放、释放、录像覆盖与切片读取；
//   - 同一 (phone, channel) 在会话生命周期内固定落在同一节点（粘性），
//     节点故障时才重新选点；
//   - 没有任何可用节点时返回 contracts.MediaUnavailableError，绝不静默降级。
type RemoteMedia struct {
	nodes  []MediaNode
	client *http.Client
	ttl    time.Duration
	now    func() time.Time

	mu       sync.Mutex
	sticky   map[string]stickyBinding
	sessions map[string]sessionBinding
}

type stickyBinding struct {
	node      int
	sessionID string
	expiresAt time.Time
}

type sessionBinding struct {
	node     int
	session  contracts.PlaybackSession
	stickyID string
}

// NewRemoteMedia 创建远程媒体调度器。节点列表可以为空：此时所有播放申请
// 返回 no_media_node_configured。
func NewRemoteMedia(opts RemoteMediaOptions) *RemoteMedia {
	ttl := opts.DefaultTTL
	if ttl <= 0 {
		ttl = defaultMediaLeaseTTL
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultMediaNodeTimeout
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: timeout}).DialContext,
		ResponseHeaderTimeout: timeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   16,
	}
	nodes := make([]MediaNode, 0, len(opts.Nodes))
	for index, node := range opts.Nodes {
		base := strings.TrimRight(strings.TrimSpace(node.URL), "/")
		if base == "" {
			continue
		}
		name := strings.TrimSpace(node.Name)
		if name == "" {
			name = fmt.Sprintf("media-%d", index+1)
		}
		nodes = append(nodes, MediaNode{Name: name, URL: base})
	}
	return &RemoteMedia{
		nodes:    nodes,
		client:   &http.Client{Transport: transport},
		ttl:      ttl,
		now:      time.Now,
		sticky:   map[string]stickyBinding{},
		sessions: map[string]sessionBinding{},
	}
}

// Nodes 返回节点清单快照。
func (r *RemoteMedia) Nodes() []MediaNode {
	if r == nil {
		return nil
	}
	out := make([]MediaNode, len(r.nodes))
	copy(out, r.nodes)
	return out
}

// Check 是管控角色 /readyz 的媒体依赖检查。
//
// 判定标准：至少配置一个节点、至少一个节点可达、且至少一个节点还有并发额度。
func (r *RemoteMedia) Check(ctx context.Context) error {
	if r == nil || len(r.nodes) == 0 {
		return fmt.Errorf("cluster.media.nodes is empty: playback APIs cannot schedule any media node")
	}
	var failures []string
	reachable := 0
	available := 0
	for _, status := range r.fetchStatuses(ctx) {
		if status.err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", status.node.Name, status.err))
			continue
		}
		reachable++
		if status.status.Available > 0 {
			available++
		}
	}
	if reachable == 0 {
		return fmt.Errorf("no media node is reachable: %s", strings.Join(failures, "; "))
	}
	if available == 0 {
		return fmt.Errorf("all %d reachable media nodes are at capacity", reachable)
	}
	return nil
}

// ActiveStreams 汇总所有可达节点的活跃会话数，用于指标采集。
func (r *RemoteMedia) ActiveStreams(ctx context.Context) int {
	total := 0
	for _, status := range r.fetchStatuses(ctx) {
		if status.err == nil {
			total += status.status.Active
		}
	}
	return total
}

// Allocate 实现 contracts.MediaAllocator：在选中的媒体节点上创建播放会话。
func (r *RemoteMedia) Allocate(ctx context.Context, request contracts.MediaLeaseRequest) (contracts.MediaLease, error) {
	kind := strings.TrimSpace(request.Source)
	if kind == "" {
		kind = "live"
	}
	session, err := r.CreatePlaybackSession(ctx, contracts.PlaybackSessionRequest{
		Phone:   request.Phone,
		Channel: request.Channel,
		Kind:    kind,
		TTL:     request.TTL,
	})
	if err != nil {
		return contracts.MediaLease{}, err
	}
	return contracts.MediaLease{
		LeaseID:   session.ID,
		Phone:     session.Phone,
		Channel:   session.Channel,
		Endpoint:  session.Endpoint,
		ExpiresAt: session.ExpiresAt,
	}, nil
}

// Release 实现 contracts.MediaAllocator。
func (r *RemoteMedia) Release(ctx context.Context, leaseID string) error {
	return r.ReleasePlaybackSession(ctx, leaseID)
}

// CreatePlaybackSession 选择节点并创建播放会话。
func (r *RemoteMedia) CreatePlaybackSession(ctx context.Context, request contracts.PlaybackSessionRequest) (contracts.PlaybackSession, error) {
	if r == nil || len(r.nodes) == 0 {
		return contracts.PlaybackSession{}, &contracts.MediaUnavailableError{
			Code:   contracts.MediaCodeNoNodes,
			Reason: "cluster.media.nodes is empty",
		}
	}
	ttl := request.TTL
	if ttl <= 0 {
		ttl = r.ttl
	}
	key := sessionKey(request.Phone, request.Channel)
	statuses := r.fetchStatuses(ctx)

	// 先恢复粘性：会话可能是在管控重启前创建的，此时本地映射为空。
	if existing, node, ok := findLiveSession(statuses, request.Phone, request.Channel); ok {
		r.remember(key, node, existing)
		return contracts.PlaybackSession{}, &contracts.PlaybackSessionConflictError{Existing: existing}
	}
	if node, ok := r.stickyNode(key); ok {
		if status, found := statusAt(statuses, node); found && status.err == nil {
			if _, exists := findSessionOn(status.status, request.Phone, request.Channel); !exists {
				r.dropSticky(key)
			}
		}
	}

	candidates, failures := selectCandidates(statuses, r.stickyIndex(key))
	if len(candidates) == 0 {
		reason := "no media node can accept a new session"
		if len(failures) > 0 {
			reason += ": " + strings.Join(failures, "; ")
		}
		return contracts.PlaybackSession{}, &contracts.MediaUnavailableError{
			Code:   contracts.MediaCodeNodesUnavailable,
			Reason: reason,
		}
	}

	for _, index := range candidates {
		session, err := r.createOnNode(ctx, index, request, ttl)
		if err == nil {
			r.remember(key, index, session)
			log.Info().
				Str("node", r.nodes[index].Name).
				Str("node_url", r.nodes[index].URL).
				Str("phone", request.Phone).
				Uint8("channel", request.Channel).
				Str("session", session.ID).
				Msg("Playback session created on remote media node")
			return session, nil
		}
		var conflict *contracts.PlaybackSessionConflictError
		if errors.As(err, &conflict) {
			r.remember(key, index, conflict.Existing)
			return contracts.PlaybackSession{}, conflict
		}
		failures = append(failures, fmt.Sprintf("%s: %v", r.nodes[index].Name, err))
		log.Warn().Err(err).Str("node", r.nodes[index].Name).Msg("Fail to create playback session on media node")
	}
	return contracts.PlaybackSession{}, &contracts.MediaUnavailableError{
		Code:   contracts.MediaCodeNodesUnavailable,
		Reason: "all media node attempts failed: " + strings.Join(failures, "; "),
	}
}

// ReleasePlaybackSession 释放播放会话；会话不存在按幂等成功处理。
func (r *RemoteMedia) ReleasePlaybackSession(ctx context.Context, sessionID string) error {
	if r == nil || strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session id is required")
	}
	if len(r.nodes) == 0 {
		return &contracts.MediaUnavailableError{
			Code:   contracts.MediaCodeNoNodes,
			Reason: "cluster.media.nodes is empty",
		}
	}
	var order []int
	if binding, ok := r.lookup(sessionID); ok {
		order = append(order, binding.node)
	}
	for index := range r.nodes {
		if len(order) > 0 && order[0] == index {
			continue
		}
		order = append(order, index)
	}
	var failures []string
	notFound := 0
	for _, index := range order {
		statusCode, err := r.deleteOnNode(ctx, index, sessionID)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", r.nodes[index].Name, err))
			continue
		}
		switch statusCode {
		case http.StatusNoContent, http.StatusOK:
			r.forget(sessionID)
			return nil
		case http.StatusNotFound:
			notFound++
		default:
			failures = append(failures, fmt.Sprintf("%s: unexpected status %d", r.nodes[index].Name, statusCode))
		}
	}
	if notFound == len(r.nodes) {
		// 全部节点都明确回答不存在：视为已经释放，删除本地残留映射。
		r.forget(sessionID)
		return nil
	}
	return fmt.Errorf("fail to release playback session %s: %s", sessionID, strings.Join(failures, "; "))
}

// PlaybackSession 返回会话快照；本地没有映射时向所有节点查询一次。
func (r *RemoteMedia) PlaybackSession(ctx context.Context, sessionID string) (contracts.PlaybackSession, bool) {
	if r == nil || len(r.nodes) == 0 {
		return contracts.PlaybackSession{}, false
	}
	if binding, ok := r.lookup(sessionID); ok {
		return binding.session, true
	}
	for _, status := range r.fetchStatuses(ctx) {
		if status.err != nil {
			continue
		}
		for _, session := range status.status.sessionsView() {
			if session.ID != sessionID {
				continue
			}
			mapped := session
			r.remember(sessionKey(mapped.Phone, mapped.Channel), status.index, mapped)
			return mapped, true
		}
	}
	return contracts.PlaybackSession{}, false
}

// RecordingCoverage 汇总所有媒体节点的录像覆盖；节点间区间取并集。
func (r *RemoteMedia) RecordingCoverage(ctx context.Context, phone string, channel uint8, start, end time.Time) (contracts.RecordingCoverage, error) {
	if r == nil || len(r.nodes) == 0 {
		return contracts.RecordingCoverage{}, &contracts.MediaUnavailableError{
			Code:   contracts.MediaCodeNoNodes,
			Reason: "cluster.media.nodes is empty",
		}
	}
	var merged contracts.RecordingCoverage
	merged.Requested = contracts.TimeRange{Start: start, End: end}
	seenSegments := map[string]bool{}
	var available []contracts.TimeRange
	var failures []string
	reachable := 0
	for _, node := range r.nodes {
		coverage, err := r.fetchCoverage(ctx, node, phone, channel, start, end)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", node.Name, err))
			continue
		}
		reachable++
		for _, segment := range coverage.Segments {
			if seenSegments[segment.ID] {
				continue
			}
			seenSegments[segment.ID] = true
			merged.Segments = append(merged.Segments, segment)
		}
		available = append(available, coverage.Available...)
	}
	if reachable == 0 {
		return contracts.RecordingCoverage{}, fmt.Errorf("no media node returned recording coverage: %s", strings.Join(failures, "; "))
	}
	sort.Slice(merged.Segments, func(i, j int) bool {
		return merged.Segments[i].Start.Before(merged.Segments[j].Start)
	})
	merged.Available = mergeRanges(available)
	cursor := start
	for _, span := range merged.Available {
		if cursor.Before(span.Start) {
			merged.Missing = append(merged.Missing, contracts.TimeRange{Start: cursor, End: span.Start})
		}
		if span.End.After(cursor) {
			cursor = span.End
		}
	}
	if cursor.Before(end) {
		merged.Missing = append(merged.Missing, contracts.TimeRange{Start: cursor, End: end})
	}
	merged.Playable = len(merged.Missing) == 0
	return merged, nil
}

// OpenRecordingSegment 在持有该切片的媒体节点上打开录像内容。
func (r *RemoteMedia) OpenRecordingSegment(ctx context.Context, phone, segmentID string, offset, length int64) (io.ReadCloser, int64, error) {
	if r == nil || len(r.nodes) == 0 {
		return nil, 0, &contracts.MediaUnavailableError{
			Code:   contracts.MediaCodeNoNodes,
			Reason: "cluster.media.nodes is empty",
		}
	}
	var failures []string
	for _, node := range r.nodes {
		reader, size, statusCode, err := r.openSegment(ctx, node, phone, segmentID, offset, length)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", node.Name, err))
			continue
		}
		switch statusCode {
		case http.StatusOK:
			return reader, size, nil
		case http.StatusRequestedRangeNotSatisfiable:
			return nil, 0, contracts.ErrRangeNotSatisfiable
		case http.StatusNotFound:
			continue
		default:
			_ = reader.Close()
			failures = append(failures, fmt.Sprintf("%s: unexpected status %d", node.Name, statusCode))
		}
	}
	if len(failures) == 0 {
		return nil, 0, contracts.ErrRecordingNotFound
	}
	return nil, 0, fmt.Errorf("fail to open recording segment %s: %s", segmentID, strings.Join(failures, "; "))
}

func (r *RemoteMedia) createOnNode(ctx context.Context, index int, request contracts.PlaybackSessionRequest, ttl time.Duration) (contracts.PlaybackSession, error) {
	body := map[string]any{
		"phone":      request.Phone,
		"channel":    request.Channel,
		"kind":       request.Kind,
		"ttlSeconds": int(ttl.Seconds()),
	}
	var info remoteSessionInfo
	status, raw, err := r.doJSON(ctx, http.MethodPost, r.nodes[index].URL+"/internal/media/sessions", body)
	if err != nil {
		return contracts.PlaybackSession{}, err
	}
	switch status {
	case http.StatusCreated, http.StatusOK:
		if err := json.Unmarshal(raw, &info); err != nil {
			return contracts.PlaybackSession{}, fmt.Errorf("decode media session: %w", err)
		}
		return info.toContract(), nil
	case http.StatusConflict:
		var conflict remoteErrorBody
		_ = json.Unmarshal(raw, &conflict)
		existing := contracts.PlaybackSession{
			Phone:   request.Phone,
			Channel: request.Channel,
			Kind:    request.Kind,
		}
		if conflict.Existing != nil {
			existing.ID = conflict.Existing.SessionID
			existing.Kind = string(conflict.Existing.Kind)
			existing.ExpiresAt = conflict.Existing.ExpiresAt
			existing.FLVPath = conflict.Existing.FLVPath
		}
		return contracts.PlaybackSession{}, &contracts.PlaybackSessionConflictError{Existing: existing}
	default:
		var failure remoteErrorBody
		_ = json.Unmarshal(raw, &failure)
		if failure.Message == "" {
			failure.Message = string(raw)
		}
		return contracts.PlaybackSession{}, fmt.Errorf("media node returned %d: %s", status, failure.Message)
	}
}

func (r *RemoteMedia) deleteOnNode(ctx context.Context, index int, sessionID string) (int, error) {
	status, _, err := r.doJSON(ctx, http.MethodDelete,
		r.nodes[index].URL+"/internal/media/sessions/"+url.PathEscape(sessionID), nil)
	return status, err
}

func (r *RemoteMedia) fetchCoverage(ctx context.Context, node MediaNode, phone string, channel uint8, start, end time.Time) (contracts.RecordingCoverage, error) {
	query := url.Values{}
	query.Set("phone", phone)
	query.Set("channel", strconv.Itoa(int(channel)))
	query.Set("start", start.Format(time.RFC3339Nano))
	query.Set("end", end.Format(time.RFC3339Nano))
	status, raw, err := r.doJSON(ctx, http.MethodGet, node.URL+"/internal/media/recordings/coverage?"+query.Encode(), nil)
	if err != nil {
		return contracts.RecordingCoverage{}, err
	}
	if status != http.StatusOK {
		return contracts.RecordingCoverage{}, fmt.Errorf("media node returned %d: %s", status, strings.TrimSpace(string(raw)))
	}
	var coverage contracts.RecordingCoverage
	if err := json.Unmarshal(raw, &coverage); err != nil {
		return contracts.RecordingCoverage{}, fmt.Errorf("decode recording coverage: %w", err)
	}
	return coverage, nil
}

func (r *RemoteMedia) openSegment(ctx context.Context, node MediaNode, phone, segmentID string, offset, length int64) (io.ReadCloser, int64, int, error) {
	query := url.Values{}
	query.Set("offset", strconv.FormatInt(offset, 10))
	if length > 0 {
		query.Set("length", strconv.FormatInt(length, 10))
	}
	endpoint := fmt.Sprintf("%s/internal/media/recordings/segments/%s/%s?%s",
		node.URL, url.PathEscape(phone), url.PathEscape(segmentID), query.Encode())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, 0, err
	}
	response, err := r.client.Do(request)
	if err != nil {
		return nil, 0, 0, err
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, 0, response.StatusCode, nil
	}
	size, parseErr := strconv.ParseInt(response.Header.Get("X-Segment-Size"), 10, 64)
	if parseErr != nil {
		size = -1
	}
	return response.Body, size, response.StatusCode, nil
}

type nodeStatus struct {
	index  int
	node   MediaNode
	status mediaStatus
	err    error
}

type mediaStatus struct {
	NodeID        string              `json:"nodeId"`
	Sessions      []remoteSessionInfo `json:"sessions"`
	Active        int                 `json:"active"`
	MaxConcurrent int                 `json:"maxConcurrent"`
	Available     int                 `json:"available"`
}

func (s mediaStatus) sessionsView() []contracts.PlaybackSession {
	out := make([]contracts.PlaybackSession, 0, len(s.Sessions))
	for _, session := range s.Sessions {
		out = append(out, session.toContract())
	}
	return out
}

func (r *RemoteMedia) fetchStatuses(ctx context.Context) []nodeStatus {
	out := make([]nodeStatus, 0, len(r.nodes))
	for index, node := range r.nodes {
		status, err := r.fetchStatus(ctx, node)
		out = append(out, nodeStatus{index: index, node: node, status: status, err: err})
	}
	return out
}

func (r *RemoteMedia) fetchStatus(ctx context.Context, node MediaNode) (mediaStatus, error) {
	status, raw, err := r.doJSON(ctx, http.MethodGet, node.URL+"/internal/media/status", nil)
	if err != nil {
		return mediaStatus{}, err
	}
	if status != http.StatusOK {
		return mediaStatus{}, fmt.Errorf("media node returned %d: %s", status, strings.TrimSpace(string(raw)))
	}
	var parsed mediaStatus
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return mediaStatus{}, fmt.Errorf("decode media status: %w", err)
	}
	return parsed, nil
}

func (r *RemoteMedia) doJSON(ctx context.Context, method, endpoint string, body any) (int, []byte, error) {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		payload = bytes.NewReader(encoded)
	}
	callCtx, cancel := context.WithTimeout(ctx, defaultMediaNodeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(callCtx, method, endpoint, payload)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := r.client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return response.StatusCode, nil, err
	}
	return response.StatusCode, raw, nil
}

func (r *RemoteMedia) remember(key string, node int, session contracts.PlaybackSession) {
	if r == nil || session.ID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sticky[key] = stickyBinding{node: node, sessionID: session.ID, expiresAt: session.ExpiresAt}
	r.sessions[session.ID] = sessionBinding{node: node, session: session, stickyID: key}
}

func (r *RemoteMedia) lookup(sessionID string) (sessionBinding, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	binding, ok := r.sessions[sessionID]
	if !ok {
		return sessionBinding{}, false
	}
	if !binding.session.ExpiresAt.IsZero() && r.now().After(binding.session.ExpiresAt) {
		delete(r.sessions, sessionID)
		if binding.stickyID != "" {
			if sticky, found := r.sticky[binding.stickyID]; found && sticky.sessionID == sessionID {
				delete(r.sticky, binding.stickyID)
			}
		}
		return sessionBinding{}, false
	}
	return binding, true
}

func (r *RemoteMedia) forget(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	binding, ok := r.sessions[sessionID]
	if !ok {
		return
	}
	delete(r.sessions, sessionID)
	if binding.stickyID != "" {
		if sticky, found := r.sticky[binding.stickyID]; found && sticky.sessionID == sessionID {
			delete(r.sticky, binding.stickyID)
		}
	}
}

func (r *RemoteMedia) stickyNode(key string) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	binding, ok := r.sticky[key]
	if !ok {
		return 0, false
	}
	if !binding.expiresAt.IsZero() && r.now().After(binding.expiresAt) {
		delete(r.sticky, key)
		return 0, false
	}
	return binding.node, true
}

func (r *RemoteMedia) stickyIndex(key string) int {
	index, ok := r.stickyNode(key)
	if !ok {
		return -1
	}
	return index
}

func (r *RemoteMedia) dropSticky(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sticky, key)
}

func selectCandidates(statuses []nodeStatus, sticky int) ([]int, []string) {
	type candidate struct {
		index     int
		available int
		active    int
	}
	var candidates []candidate
	var failures []string
	for _, status := range statuses {
		if status.err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", status.node.Name, status.err))
			continue
		}
		if status.status.Available <= 0 {
			failures = append(failures, fmt.Sprintf("%s: at capacity (%d/%d)",
				status.node.Name, status.status.Active, status.status.MaxConcurrent))
			continue
		}
		candidates = append(candidates, candidate{
			index:     status.index,
			available: status.status.Available,
			active:    status.status.Active,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].index == sticky {
			return true
		}
		if candidates[j].index == sticky {
			return false
		}
		// 优先剩余额度多、当前负载低的节点；额度相同按配置顺序保证可预测。
		if candidates[i].available != candidates[j].available {
			return candidates[i].available > candidates[j].available
		}
		if candidates[i].active != candidates[j].active {
			return candidates[i].active < candidates[j].active
		}
		return candidates[i].index < candidates[j].index
	})
	out := make([]int, 0, len(candidates))
	for _, item := range candidates {
		out = append(out, item.index)
	}
	return out, failures
}

func statusAt(statuses []nodeStatus, index int) (nodeStatus, bool) {
	for _, status := range statuses {
		if status.index == index {
			return status, true
		}
	}
	return nodeStatus{}, false
}

func findSessionOn(status mediaStatus, phone string, channel uint8) (contracts.PlaybackSession, bool) {
	now := time.Now()
	for _, session := range status.sessionsView() {
		if session.Phone != phone || session.Channel != channel {
			continue
		}
		if !session.ExpiresAt.IsZero() && now.After(session.ExpiresAt) {
			continue
		}
		return session, true
	}
	return contracts.PlaybackSession{}, false
}

func findLiveSession(statuses []nodeStatus, phone string, channel uint8) (contracts.PlaybackSession, int, bool) {
	for _, status := range statuses {
		if status.err != nil {
			continue
		}
		if session, ok := findSessionOn(status.status, phone, channel); ok {
			return session, status.index, true
		}
	}
	return contracts.PlaybackSession{}, 0, false
}

func mergeRanges(ranges []contracts.TimeRange) []contracts.TimeRange {
	if len(ranges) == 0 {
		return nil
	}
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].Start.Before(ranges[j].Start)
	})
	out := []contracts.TimeRange{ranges[0]}
	for _, current := range ranges[1:] {
		last := &out[len(out)-1]
		if !current.Start.After(last.End) {
			if current.End.After(last.End) {
				last.End = current.End
			}
			continue
		}
		out = append(out, current)
	}
	return out
}

func sessionKey(phone string, channel uint8) string {
	return fmt.Sprintf("%s/%d", phone, channel)
}

type remoteEndpoint struct {
	Protocol string `json:"protocol"`
	URL      string `json:"url"`
}

type remoteSessionInfo struct {
	ID          string           `json:"sessionId"`
	Phone       string           `json:"phone"`
	Channel     uint8            `json:"channel"`
	Kind        string           `json:"kind"`
	ExpiresAt   time.Time        `json:"expiresAt"`
	FLVPath     string           `json:"flvPath"`
	ServerIP    string           `json:"serverIp"`
	TCPPort     uint16           `json:"tcpPort"`
	UDPPort     uint16           `json:"udpPort"`
	Output      string           `json:"output"`
	PlaybackURL string           `json:"playbackUrl"`
	Endpoints   []remoteEndpoint `json:"endpoints"`
}

func (s remoteSessionInfo) toContract() contracts.PlaybackSession {
	out := contracts.PlaybackSession{
		ID:          s.ID,
		Phone:       s.Phone,
		Channel:     s.Channel,
		Kind:        s.Kind,
		ExpiresAt:   s.ExpiresAt,
		FLVPath:     s.FLVPath,
		Output:      s.Output,
		PlaybackURL: s.PlaybackURL,
		Endpoint: contracts.MediaEndpoint{
			IP:      s.ServerIP,
			TCPPort: s.TCPPort,
			UDPPort: s.UDPPort,
		},
	}
	for _, endpoint := range s.Endpoints {
		out.Endpoints = append(out.Endpoints, contracts.PlaybackEndpoint{
			Protocol: endpoint.Protocol,
			URL:      endpoint.URL,
		})
	}
	return out
}

type remoteErrorBody struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Existing *struct {
		SessionID string    `json:"sessionId"`
		Kind      string    `json:"kind"`
		ExpiresAt time.Time `json:"expiresAt"`
		FLVPath   string    `json:"flvPath"`
	} `json:"existing,omitempty"`
}
