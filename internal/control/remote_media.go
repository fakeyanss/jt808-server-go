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

	clockpkg "github.com/fakeyanss/jt808-server-go/internal/clock"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// 管控调用媒体节点内部 HTTP 的默认超时。
const (
	defaultMediaNodeTimeout = 3 * time.Second
	defaultMediaLeaseTTL    = 10 * time.Minute
)

// MediaNode 是一个可被管控调度的媒体节点（静态清单形态）。
type MediaNode struct {
	Name string
	URL  string
}

// RemoteMediaOptions 是远程媒体调度器的构造参数。
type RemoteMediaOptions struct {
	// Nodes 是静态媒体节点清单，作为注册表为空时的兜底。
	Nodes []MediaNode
	// Timeout 是单次内部 HTTP 调用超时；<=0 时使用默认值。
	Timeout time.Duration
	// DefaultTTL 是播放会话的默认存活时长；<=0 时使用 10 分钟。
	DefaultTTL time.Duration
	// NodeRepo 是媒体节点注册表；非空时以注册表为分配依据。
	NodeRepo repository.MediaNodeRepository
	// Clock 是进程内唯一时钟源；为空时使用系统时钟。
	Clock clockpkg.Clock
	// Metrics 记录无可用节点与分配失败。
	Metrics *observability.Metrics
}

// RemoteMedia 是跨进程的媒体调度器：
//
//   - 实现 contracts.MediaAllocator：Allocate 在媒体节点上创建播放会话；
//   - 实现 contracts.PlaybackLibrary：播放、释放、录像覆盖与切片读取；
//   - 有注册节点时以注册表为准：从“未过期、在线、容量未满”的节点中
//     挑负载最低者；注册表为空才退回静态 cluster.media.nodes；
//   - 同一 (phone, channel) 在会话生命周期内复用同一分配，不重复开流；
//   - 没有任何可用节点时返回 contracts.MediaUnavailableError，绝不静默降级。
type RemoteMedia struct {
	nodes   []MediaNode
	repo    repository.MediaNodeRepository
	client  *http.Client
	ttl     time.Duration
	clock   clockpkg.Clock
	metrics *observability.Metrics

	mu       sync.Mutex
	sticky   map[string]stickyBinding
	sessions map[string]sessionBinding
	// allocLocks 按 (phone, channel) 分片，串行化同一逻辑通道的并发申请，
	// 避免两个请求同时判定“还没有分配”而各自开出第二条流。
	allocLocks [64]sync.Mutex
}

// mediaTarget 是一次调度看到的媒体节点。
//
// Registered=true 表示节点来自注册表，带负载与容量，需要原子占位；
// Registered=false 表示静态清单，额度只能靠 /internal/media/status 判断。
type mediaTarget struct {
	ID         string
	Name       string
	URL        string
	InstanceID string
	Registered bool

	EndpointCapacity   int64
	EndpointsUsed      int64
	MaxBitrateKbps     int64
	InboundBitrateKbps int64
	ActiveChannels     int64
	// Order 是静态清单中的配置顺序，仅用于同分时保持可预测。
	Order int
}

type stickyBinding struct {
	nodeID    string
	sessionID string
	expiresAt time.Time
}

type sessionBinding struct {
	nodeID     string
	instanceID string
	// reserved 表示该会话在注册表里占用了一个端点，释放时要归还。
	reserved bool
	session  contracts.PlaybackSession
	stickyID string
}

// NewRemoteMedia 创建远程媒体调度器。节点注册表与静态清单都为空时，
// 所有播放申请返回 no_media_node_configured。
func NewRemoteMedia(opts RemoteMediaOptions) *RemoteMedia {
	ttl := opts.DefaultTTL
	if ttl <= 0 {
		ttl = defaultMediaLeaseTTL
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultMediaNodeTimeout
	}
	clock := opts.Clock
	if clock == nil {
		clock = clockpkg.System()
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
		repo:     opts.NodeRepo,
		client:   &http.Client{Transport: transport},
		ttl:      ttl,
		clock:    clock,
		metrics:  opts.Metrics,
		sticky:   map[string]stickyBinding{},
		sessions: map[string]sessionBinding{},
	}
}

func (r *RemoteMedia) now() time.Time {
	if r == nil || r.clock == nil {
		return time.Now()
	}
	return r.clock.Now()
}

// Nodes 返回静态清单快照。
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
// 有注册节点时以注册表为准：在线且仍有额度的节点至少一个；
// 注册表为空时退回静态清单，并要求至少一个节点可达且有额度。
func (r *RemoteMedia) Check(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("media allocator is not configured")
	}
	if r.repo != nil {
		nodes, err := r.repo.ListOnline(ctx, r.now())
		if err != nil {
			return fmt.Errorf("list registered media nodes: %w", err)
		}
		if len(nodes) > 0 {
			available := 0
			for _, node := range nodes {
				if repository.MediaNodeHasCapacity(node) {
					available++
				}
			}
			if available == 0 {
				return fmt.Errorf("all %d registered media nodes are at capacity", len(nodes))
			}
			return nil
		}
		if len(r.nodes) == 0 {
			return fmt.Errorf("no media node is registered: heartbeat is missing or every node has expired")
		}
	}
	if len(r.nodes) == 0 {
		return fmt.Errorf("cluster.media.nodes is empty: playback APIs cannot schedule any media node")
	}
	var failures []string
	reachable := 0
	available := 0
	for _, status := range r.fetchStatuses(ctx) {
		if status.err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", status.target.Name, status.err))
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

// ActiveStreams 汇总所有可用节点的活跃通道数，用于指标采集。
func (r *RemoteMedia) ActiveStreams(ctx context.Context) int {
	if r == nil {
		return 0
	}
	targets, err := r.registeredTargets(ctx)
	if err == nil && len(targets) > 0 {
		total := 0
		for _, target := range targets {
			total += int(target.ActiveChannels)
		}
		return total
	}
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
//
// 0x9101（实时）与 0x9201（终端回放）都走这里：先看同一 (phone, channel)
// 是否已有同类型分配，有则直接复用；否则按负载从低到高尝试候选节点，
// 注册表节点先原子占位再建会话，失败立刻归还。
func (r *RemoteMedia) CreatePlaybackSession(ctx context.Context, request contracts.PlaybackSessionRequest) (contracts.PlaybackSession, error) {
	if r == nil {
		return contracts.PlaybackSession{}, &contracts.MediaUnavailableError{
			Code:   contracts.MediaCodeNoNodes,
			Reason: "media allocator is not configured",
		}
	}
	if r.repo == nil && len(r.nodes) == 0 {
		return contracts.PlaybackSession{}, &contracts.MediaUnavailableError{
			Code:   contracts.MediaCodeNoNodes,
			Reason: "cluster.media.nodes is empty",
		}
	}
	kind := strings.TrimSpace(request.Kind)
	if kind == "" {
		kind = "live"
	}
	key := sessionKey(request.Phone, request.Channel)
	unlock := r.lockAllocation(key)
	defer unlock()

	// 同一终端同一逻辑通道已有分配：复用，不重复开流。
	if existing, ok := r.liveBinding(key); ok {
		if existing.Kind == kind {
			return existing, nil
		}
		return contracts.PlaybackSession{}, &contracts.PlaybackSessionConflictError{Existing: existing}
	}

	// 管控重启后本地映射为空：向节点查一次，避免同一通道重复开流。
	targets := r.targets(ctx)
	if existing, target, ok := r.findExistingSession(ctx, targets, request.Phone, request.Channel); ok {
		if strings.TrimSpace(existing.Kind) == "" {
			existing.Kind = kind
		}
		r.remember(key, target, existing, false)
		if existing.Kind == kind {
			return existing, nil
		}
		return contracts.PlaybackSession{}, &contracts.PlaybackSessionConflictError{Existing: existing}
	}

	candidates, failures := r.selectCandidates(ctx)
	if len(candidates) == 0 {
		if r.metrics != nil {
			r.metrics.IncMediaAllocationNoNode()
		}
		reason := "no media node can accept a new session"
		if len(failures) > 0 {
			reason += ": " + strings.Join(failures, "; ")
		}
		return contracts.PlaybackSession{}, &contracts.MediaUnavailableError{
			Code:   contracts.MediaCodeNodesUnavailable,
			Reason: reason,
		}
	}

	ttl := request.TTL
	if ttl <= 0 {
		ttl = r.ttl
	}

	for _, candidate := range candidates {
		target := candidate.target
		reserved := false
		if target.Registered {
			ok, err := r.repo.Reserve(ctx, target.ID, target.InstanceID, 1, r.now())
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s: reserve endpoint: %v", target.Name, err))
				continue
			}
			if !ok {
				// 并发分配把额度抢走了：换下一个节点，而不是复用“当前最低”。
				failures = append(failures, fmt.Sprintf("%s: endpoint reservation rejected", target.Name))
				continue
			}
			reserved = true
		}
		session, err := r.createOnTarget(ctx, target, request, ttl)
		if err == nil {
			r.remember(key, target, session, reserved)
			log.Info().
				Str("node", target.Name).
				Str("node_url", target.URL).
				Str("phone", request.Phone).
				Uint8("channel", request.Channel).
				Str("kind", kind).
				Str("session", session.ID).
				Msg("Playback session created on remote media node")
			return session, nil
		}
		var conflict *contracts.PlaybackSessionConflictError
		if errors.As(err, &conflict) {
			if reserved {
				r.releaseReservation(ctx, target)
			}
			existing := conflict.Existing
			if strings.TrimSpace(existing.Kind) == "" {
				existing.Kind = kind
			}
			if existing.ID != "" {
				r.remember(key, target, existing, false)
			}
			if existing.Kind == kind {
				// 节点上已有同一通道的会话（例如管控重启后）：直接复用。
				return existing, nil
			}
			return contracts.PlaybackSession{}, conflict
		}
		if reserved {
			r.releaseReservation(ctx, target)
		}
		failures = append(failures, fmt.Sprintf("%s: %v", target.Name, err))
		log.Warn().Err(err).Str("node", target.Name).Msg("Fail to create playback session on media node")
	}
	if r.metrics != nil {
		r.metrics.IncMediaAllocationFailed()
	}
	return contracts.PlaybackSession{}, &contracts.MediaUnavailableError{
		Code:   contracts.MediaCodeNodesUnavailable,
		Reason: "all media node attempts failed: " + strings.Join(failures, "; "),
	}
}

// ReleasePlaybackSession 释放播放会话并归还端点占位；会话不存在按幂等成功处理。
func (r *RemoteMedia) ReleasePlaybackSession(ctx context.Context, sessionID string) error {
	if r == nil {
		return fmt.Errorf("media allocator is not configured")
	}
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session id is required")
	}
	binding, hasBinding := r.lookup(sessionID)

	targets := r.targets(ctx)
	if len(targets) == 0 && !hasBinding {
		return &contracts.MediaUnavailableError{
			Code:   contracts.MediaCodeNoNodes,
			Reason: "no media node is configured",
		}
	}
	ordered := make([]mediaTarget, 0, len(targets))
	if hasBinding {
		for _, target := range targets {
			if target.ID == binding.nodeID {
				ordered = append(ordered, target)
			}
		}
	}
	for _, target := range targets {
		if hasBinding && target.ID == binding.nodeID {
			continue
		}
		ordered = append(ordered, target)
	}
	if len(ordered) == 0 {
		// 节点已注销或过期：会话随节点消失，归还占位即可。
		if hasBinding && binding.reserved {
			r.releaseReservation(ctx, mediaTarget{ID: binding.nodeID, InstanceID: binding.instanceID, Name: binding.nodeID})
		}
		r.forget(sessionID)
		return nil
	}
	var failures []string
	notFound := 0
	for _, target := range ordered {
		statusCode, err := r.deleteOnTarget(ctx, target, sessionID)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", target.Name, err))
			continue
		}
		switch statusCode {
		case http.StatusNoContent, http.StatusOK:
			if hasBinding && binding.reserved {
				r.releaseReservation(ctx, mediaTarget{ID: binding.nodeID, InstanceID: binding.instanceID, Name: binding.nodeID})
			}
			r.forget(sessionID)
			return nil
		case http.StatusNotFound:
			notFound++
		default:
			failures = append(failures, fmt.Sprintf("%s: unexpected status %d", target.Name, statusCode))
		}
	}
	if len(ordered) > 0 && notFound == len(ordered) {
		// 全部节点都明确回答不存在：视为已经释放，归还占位并删除本地残留映射。
		if hasBinding && binding.reserved {
			r.releaseReservation(ctx, mediaTarget{ID: binding.nodeID, InstanceID: binding.instanceID, Name: binding.nodeID})
		}
		r.forget(sessionID)
		return nil
	}
	// 释放失败时保留本地映射与占位，交给调用方重试或会话 TTL 自然收敛，
	// 避免节点仍在推流却提前归还额度。
	return fmt.Errorf("fail to release playback session %s: %s", sessionID, strings.Join(failures, "; "))
}

// PlaybackSession 返回会话快照；本地没有映射时向节点查询一次。
func (r *RemoteMedia) PlaybackSession(ctx context.Context, sessionID string) (contracts.PlaybackSession, bool) {
	if r == nil {
		return contracts.PlaybackSession{}, false
	}
	if binding, ok := r.lookup(sessionID); ok {
		return binding.session, true
	}
	for _, target := range r.targets(ctx) {
		status, err := r.fetchStatus(ctx, target)
		if err != nil {
			continue
		}
		for _, session := range status.sessionsView() {
			if session.ID != sessionID {
				continue
			}
			mapped := session
			r.remember(sessionKey(mapped.Phone, mapped.Channel), target, mapped, false)
			return mapped, true
		}
	}
	return contracts.PlaybackSession{}, false
}

// RecordingCoverage 汇总所有可用节点的录像覆盖；节点间区间取并集。
func (r *RemoteMedia) RecordingCoverage(ctx context.Context, phone string, channel uint8, start, end time.Time) (contracts.RecordingCoverage, error) {
	if r == nil {
		return contracts.RecordingCoverage{}, &contracts.MediaUnavailableError{
			Code:   contracts.MediaCodeNoNodes,
			Reason: "media allocator is not configured",
		}
	}
	targets := r.targets(ctx)
	if len(targets) == 0 {
		return contracts.RecordingCoverage{}, &contracts.MediaUnavailableError{
			Code:   contracts.MediaCodeNoNodes,
			Reason: "no media node is available",
		}
	}
	var merged contracts.RecordingCoverage
	merged.Requested = contracts.TimeRange{Start: start, End: end}
	seenSegments := map[string]bool{}
	var available []contracts.TimeRange
	var failures []string
	reachable := 0
	for _, target := range targets {
		coverage, err := r.fetchCoverage(ctx, target, phone, channel, start, end)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", target.Name, err))
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
	if r == nil {
		return nil, 0, &contracts.MediaUnavailableError{
			Code:   contracts.MediaCodeNoNodes,
			Reason: "media allocator is not configured",
		}
	}
	targets := r.targets(ctx)
	if len(targets) == 0 {
		return nil, 0, &contracts.MediaUnavailableError{
			Code:   contracts.MediaCodeNoNodes,
			Reason: "no media node is available",
		}
	}
	var failures []string
	for _, target := range targets {
		reader, size, statusCode, err := r.openSegment(ctx, target, phone, segmentID, offset, length)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", target.Name, err))
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
			failures = append(failures, fmt.Sprintf("%s: unexpected status %d", target.Name, statusCode))
		}
	}
	if len(failures) == 0 {
		return nil, 0, contracts.ErrRecordingNotFound
	}
	return nil, 0, fmt.Errorf("fail to open recording segment %s: %s", segmentID, strings.Join(failures, "; "))
}

// mediaCandidate 是排序后的候选节点。
type mediaCandidate struct {
	target      mediaTarget
	bitrateUtil float64
	channelUtil float64
	// order 是静态清单的配置顺序，用于同分时的可预测性。
	order int
}

// selectCandidates 返回按优先级排序的候选节点。
//
// 注册表节点：码率利用率 → 端点利用率 → node_id，负载最低者在前。
// 静态清单：剩余额度多 → 活跃数少 → 配置顺序（沿用旧行为）。
func (r *RemoteMedia) selectCandidates(ctx context.Context) ([]mediaCandidate, []string) {
	targets, err := r.registeredTargets(ctx)
	if err != nil {
		return nil, []string{"list media nodes: " + err.Error()}
	}
	if len(targets) > 0 {
		candidates := make([]mediaCandidate, 0, len(targets))
		var failures []string
		for _, target := range targets {
			if !mediaTargetHasCapacity(target) {
				failures = append(failures, fmt.Sprintf("%s: at capacity (%d/%d endpoints, %d/%d kbps)",
					target.Name, target.EndpointsUsed, target.EndpointCapacity,
					target.InboundBitrateKbps, target.MaxBitrateKbps))
				continue
			}
			candidates = append(candidates, mediaCandidate{
				target:      target,
				bitrateUtil: mediaBitrateUtilization(target),
				channelUtil: mediaChannelUtilization(target),
			})
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].bitrateUtil != candidates[j].bitrateUtil {
				return candidates[i].bitrateUtil < candidates[j].bitrateUtil
			}
			if candidates[i].channelUtil != candidates[j].channelUtil {
				return candidates[i].channelUtil < candidates[j].channelUtil
			}
			return candidates[i].target.ID < candidates[j].target.ID
		})
		return candidates, failures
	}

	// 静态清单没有心跳负载，只能靠节点状态接口判断额度。
	statuses := r.fetchStatuses(ctx)
	candidates := make([]mediaCandidate, 0, len(statuses))
	var failures []string
	for _, status := range statuses {
		if status.err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", status.target.Name, status.err))
			continue
		}
		if status.status.Available <= 0 {
			failures = append(failures, fmt.Sprintf("%s: at capacity (%d/%d)",
				status.target.Name, status.status.Active, status.status.MaxConcurrent))
			continue
		}
		candidates = append(candidates, mediaCandidate{
			target:      status.target,
			bitrateUtil: float64(status.status.Active),
			channelUtil: -float64(status.status.Available),
			order:       status.target.Order,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		// 剩余额度多（channelUtil 更小）优先，其次活跃数少，最后按配置顺序。
		if candidates[i].channelUtil != candidates[j].channelUtil {
			return candidates[i].channelUtil < candidates[j].channelUtil
		}
		if candidates[i].bitrateUtil != candidates[j].bitrateUtil {
			return candidates[i].bitrateUtil < candidates[j].bitrateUtil
		}
		return candidates[i].order < candidates[j].order
	})
	return candidates, failures
}

// registeredTargets 返回注册表中在线且未过期的节点。
// 未配置注册表或注册表为空时返回空切片，由调用方退回静态清单。
func (r *RemoteMedia) registeredTargets(ctx context.Context) ([]mediaTarget, error) {
	if r == nil || r.repo == nil {
		return nil, nil
	}
	nodes, err := r.repo.ListOnline(ctx, r.now())
	if err != nil {
		log.Warn().Err(err).Msg("Fail to list registered media nodes; falling back to static node list")
		return nil, err
	}
	out := make([]mediaTarget, 0, len(nodes))
	for _, node := range nodes {
		if node == nil || strings.TrimSpace(node.InternalURL) == "" {
			continue
		}
		out = append(out, mediaTarget{
			ID:                 node.NodeID,
			Name:               node.NodeID,
			URL:                strings.TrimRight(node.InternalURL, "/"),
			InstanceID:         node.InstanceID,
			Registered:         true,
			EndpointCapacity:   node.EndpointCapacity,
			EndpointsUsed:      node.EndpointsUsed,
			MaxBitrateKbps:     node.MaxBitrateKbps,
			InboundBitrateKbps: node.InboundBitrateKbps,
			ActiveChannels:     node.ActiveChannels,
		})
	}
	return out, nil
}

// staticTargets 把静态清单转成统一的目标描述。
func (r *RemoteMedia) staticTargets() []mediaTarget {
	if r == nil {
		return nil
	}
	out := make([]mediaTarget, 0, len(r.nodes))
	for index, node := range r.nodes {
		out = append(out, mediaTarget{ID: node.Name, Name: node.Name, URL: node.URL, Order: index})
	}
	return out
}

// targets 返回本次调度可见的节点：注册表优先，为空时退回静态清单。
func (r *RemoteMedia) targets(ctx context.Context) []mediaTarget {
	targets, err := r.registeredTargets(ctx)
	if err == nil && len(targets) > 0 {
		return targets
	}
	return r.staticTargets()
}

func mediaTargetHasCapacity(target mediaTarget) bool {
	if target.EndpointCapacity > 0 && target.EndpointsUsed >= target.EndpointCapacity {
		return false
	}
	if target.MaxBitrateKbps > 0 && target.InboundBitrateKbps >= target.MaxBitrateKbps {
		return false
	}
	return true
}

// mediaBitrateUtilization 是码率利用率；未配置码率上限时返回 0。
func mediaBitrateUtilization(target mediaTarget) float64 {
	if target.MaxBitrateKbps <= 0 {
		return 0
	}
	return float64(target.InboundBitrateKbps) / float64(target.MaxBitrateKbps)
}

// mediaChannelUtilization 是端点利用率；容量不限时返回 0。
func mediaChannelUtilization(target mediaTarget) float64 {
	if target.EndpointCapacity <= 0 {
		return 0
	}
	return float64(target.EndpointsUsed) / float64(target.EndpointCapacity)
}

func (r *RemoteMedia) createOnTarget(ctx context.Context, target mediaTarget, request contracts.PlaybackSessionRequest, ttl time.Duration) (contracts.PlaybackSession, error) {
	body := map[string]any{
		"phone":      request.Phone,
		"channel":    request.Channel,
		"kind":       request.Kind,
		"ttlSeconds": int(ttl.Seconds()),
	}
	var info remoteSessionInfo
	status, raw, err := r.doJSON(ctx, http.MethodPost, target.URL+"/internal/media/sessions", body)
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

func (r *RemoteMedia) deleteOnTarget(ctx context.Context, target mediaTarget, sessionID string) (int, error) {
	status, _, err := r.doJSON(ctx, http.MethodDelete,
		target.URL+"/internal/media/sessions/"+url.PathEscape(sessionID), nil)
	return status, err
}

func (r *RemoteMedia) releaseReservation(ctx context.Context, target mediaTarget) {
	if r == nil || r.repo == nil || target.ID == "" || target.InstanceID == "" {
		return
	}
	if err := r.repo.Release(ctx, target.ID, target.InstanceID, 1, r.now()); err != nil {
		log.Warn().Err(err).Str("node", target.ID).Msg("Fail to release media endpoint reservation")
	}
}

func (r *RemoteMedia) fetchCoverage(ctx context.Context, target mediaTarget, phone string, channel uint8, start, end time.Time) (contracts.RecordingCoverage, error) {
	query := url.Values{}
	query.Set("phone", phone)
	query.Set("channel", strconv.Itoa(int(channel)))
	query.Set("start", start.Format(time.RFC3339Nano))
	query.Set("end", end.Format(time.RFC3339Nano))
	status, raw, err := r.doJSON(ctx, http.MethodGet, target.URL+"/internal/media/recordings/coverage?"+query.Encode(), nil)
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

func (r *RemoteMedia) openSegment(ctx context.Context, target mediaTarget, phone, segmentID string, offset, length int64) (io.ReadCloser, int64, int, error) {
	query := url.Values{}
	query.Set("offset", strconv.FormatInt(offset, 10))
	if length > 0 {
		query.Set("length", strconv.FormatInt(length, 10))
	}
	endpoint := fmt.Sprintf("%s/internal/media/recordings/segments/%s/%s?%s",
		target.URL, url.PathEscape(phone), url.PathEscape(segmentID), query.Encode())
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
	target mediaTarget
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
	targets := r.staticTargets()
	out := make([]nodeStatus, 0, len(targets))
	for _, target := range targets {
		status, err := r.fetchStatus(ctx, target)
		out = append(out, nodeStatus{target: target, status: status, err: err})
	}
	return out
}

// findExistingSession 在候选节点上查找同一 (phone, channel) 的有效会话。
//
// 只在本地没有映射时调用：管控重启或另一副本已建流时，节点状态是唯一的
// 复用依据，避免同一逻辑通道开出第二条流。
func (r *RemoteMedia) findExistingSession(ctx context.Context, targets []mediaTarget, phone string, channel uint8) (contracts.PlaybackSession, mediaTarget, bool) {
	now := r.now()
	for _, target := range targets {
		status, err := r.fetchStatus(ctx, target)
		if err != nil {
			continue
		}
		for _, session := range status.sessionsView() {
			if session.Phone != phone || session.Channel != channel {
				continue
			}
			if !session.ExpiresAt.IsZero() && now.After(session.ExpiresAt) {
				continue
			}
			return session, target, true
		}
	}
	return contracts.PlaybackSession{}, mediaTarget{}, false
}

func (r *RemoteMedia) fetchStatus(ctx context.Context, target mediaTarget) (mediaStatus, error) {
	status, raw, err := r.doJSON(ctx, http.MethodGet, target.URL+"/internal/media/status", nil)
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

func (r *RemoteMedia) remember(key string, target mediaTarget, session contracts.PlaybackSession, reserved bool) {
	if r == nil || session.ID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sticky[key] = stickyBinding{nodeID: target.ID, sessionID: session.ID, expiresAt: session.ExpiresAt}
	r.sessions[session.ID] = sessionBinding{
		nodeID:     target.ID,
		instanceID: target.InstanceID,
		reserved:   reserved,
		session:    session,
		stickyID:   key,
	}
}

// liveBinding 返回同一 (phone, channel) 仍有效的本地会话。
func (r *RemoteMedia) liveBinding(key string) (contracts.PlaybackSession, bool) {
	r.mu.Lock()
	sticky, ok := r.sticky[key]
	r.mu.Unlock()
	if !ok {
		return contracts.PlaybackSession{}, false
	}
	if !sticky.expiresAt.IsZero() && r.now().After(sticky.expiresAt) {
		r.dropSticky(key)
		return contracts.PlaybackSession{}, false
	}
	binding, ok := r.lookup(sticky.sessionID)
	if !ok {
		r.dropSticky(key)
		return contracts.PlaybackSession{}, false
	}
	return binding.session, true
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

func (r *RemoteMedia) dropSticky(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sticky, key)
}

// lockAllocation 获取同一 (phone, channel) 的申请锁，返回释放函数。
func (r *RemoteMedia) lockAllocation(key string) func() {
	shard := &r.allocLocks[allocationShard(key, len(r.allocLocks))]
	shard.Lock()
	return shard.Unlock
}

func allocationShard(key string, shards int) int {
	if shards <= 0 {
		return 0
	}
	// FNV-1a：只用于分片，不参与任何持久化标识。
	var hash uint32 = 2166136261
	for index := 0; index < len(key); index++ {
		hash ^= uint32(key[index])
		hash *= 16777619
	}
	return int(hash % uint32(shards))
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
