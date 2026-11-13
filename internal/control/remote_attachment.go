package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

// defaultAttachmentNodeTimeout 是管控调用附件节点状态接口的超时。
const defaultAttachmentNodeTimeout = 3 * time.Second

// AttachmentNode 是一个可被管控调度的附件节点。
type AttachmentNode struct {
	Name string
	URL  string
}

// RemoteAttachmentOptions 是远程附件调度器的构造参数。
type RemoteAttachmentOptions struct {
	Nodes      []AttachmentNode
	Timeout    time.Duration
	DefaultTTL time.Duration
}

// RemoteAttachment 按节点状态选择附件节点，并保证同一 (phone, alarm) 粘性。
//
// 没有可用节点时返回 contracts.AttachmentUnavailableError，
// API 据此返回稳定的 503 + code，而不是静默成功。
type RemoteAttachment struct {
	nodes  []AttachmentNode
	client *http.Client
	ttl    time.Duration
	now    func() time.Time

	mu     sync.Mutex
	sticky map[string]attachmentBinding
	leases map[string]attachmentBinding
}

type attachmentBinding struct {
	node      int
	key       string
	endpoint  contracts.AttachmentEndpoint
	expiresAt time.Time
}

// NewRemoteAttachment 创建远程附件调度器。
func NewRemoteAttachment(opts RemoteAttachmentOptions) *RemoteAttachment {
	ttl := opts.DefaultTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultAttachmentNodeTimeout
	}
	nodes := make([]AttachmentNode, 0, len(opts.Nodes))
	for index, node := range opts.Nodes {
		base := strings.TrimRight(strings.TrimSpace(node.URL), "/")
		if base == "" {
			continue
		}
		name := strings.TrimSpace(node.Name)
		if name == "" {
			name = fmt.Sprintf("attachment-%d", index+1)
		}
		nodes = append(nodes, AttachmentNode{Name: name, URL: base})
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: timeout}).DialContext,
		ResponseHeaderTimeout: timeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   8,
	}
	return &RemoteAttachment{
		nodes:  nodes,
		client: &http.Client{Transport: transport},
		ttl:    ttl,
		now:    time.Now,
		sticky: map[string]attachmentBinding{},
		leases: map[string]attachmentBinding{},
	}
}

// Nodes 返回节点清单快照。
func (r *RemoteAttachment) Nodes() []AttachmentNode {
	if r == nil {
		return nil
	}
	out := make([]AttachmentNode, len(r.nodes))
	copy(out, r.nodes)
	return out
}

// Check 是 control /readyz 的附件依赖检查。
func (r *RemoteAttachment) Check(ctx context.Context) error {
	if r == nil || len(r.nodes) == 0 {
		return fmt.Errorf("cluster.attachment.nodes is empty: automatic 0x9208 and attachment APIs cannot schedule any node")
	}
	statuses := r.fetchStatuses(ctx)
	reachable, available := 0, 0
	var failures []string
	for _, status := range statuses {
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
		return fmt.Errorf("no attachment node is reachable: %s", strings.Join(failures, "; "))
	}
	if available == 0 {
		return fmt.Errorf("all %d reachable attachment nodes are at capacity", reachable)
	}
	return nil
}

// Allocate 实现 contracts.AttachmentAllocator。
func (r *RemoteAttachment) Allocate(ctx context.Context, request contracts.AttachmentLeaseRequest) (contracts.AttachmentLease, error) {
	if r == nil || len(r.nodes) == 0 {
		return contracts.AttachmentLease{}, &contracts.AttachmentUnavailableError{
			Code:   contracts.AttachmentCodeNoNodes,
			Reason: "cluster.attachment.nodes is empty",
		}
	}
	key := attachmentKey(request.Phone, request.AlarmID)
	statuses := r.fetchStatuses(ctx)
	if node, ok := r.stickyNode(key); ok {
		if status, found := attachmentStatusAt(statuses, node); found && status.err == nil {
			endpoint := status.status.endpoint(status.node)
			if endpoint.IP != "" && (endpoint.TCPPort != 0 || endpoint.UDPPort != 0) {
				ttl := request.TTL
				if ttl <= 0 {
					ttl = r.ttl
				}
				lease := contracts.AttachmentLease{
					LeaseID:   "att-" + key,
					Phone:     request.Phone,
					Endpoint:  endpoint,
					ExpiresAt: r.now().Add(ttl),
				}
				r.remember(key, lease.LeaseID, node, endpoint, lease.ExpiresAt)
				return lease, nil
			}
		}
		r.dropSticky(key)
	}

	var candidates []nodeAttachmentStatus
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
		candidates = append(candidates, status)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].status.Available != candidates[j].status.Available {
			return candidates[i].status.Available > candidates[j].status.Available
		}
		if candidates[i].status.Active != candidates[j].status.Active {
			return candidates[i].status.Active < candidates[j].status.Active
		}
		return candidates[i].index < candidates[j].index
	})
	for _, candidate := range candidates {
		endpoint := candidate.status.endpoint(candidate.node)
		if endpoint.IP == "" || (endpoint.TCPPort == 0 && endpoint.UDPPort == 0) {
			failures = append(failures, fmt.Sprintf("%s: no advertised tcp/udp endpoint", candidate.node.Name))
			continue
		}
		ttl := request.TTL
		if ttl <= 0 {
			ttl = r.ttl
		}
		expiresAt := r.now().Add(ttl)
		lease := contracts.AttachmentLease{
			LeaseID:   "att-" + key,
			Phone:     request.Phone,
			Endpoint:  endpoint,
			ExpiresAt: expiresAt,
		}
		r.remember(key, lease.LeaseID, candidate.index, endpoint, expiresAt)
		log.Info().Str("node", candidate.node.Name).Str("phone", request.Phone).
			Str("alarm", request.AlarmID).Str("ip", endpoint.IP).
			Uint16("tcp", endpoint.TCPPort).Uint16("udp", endpoint.UDPPort).
			Msg("Attachment endpoint allocated on remote node")
		return lease, nil
	}
	reason := "no attachment node can accept a new upload"
	if len(failures) > 0 {
		reason += ": " + strings.Join(failures, "; ")
	}
	return contracts.AttachmentLease{}, &contracts.AttachmentUnavailableError{
		Code:   contracts.AttachmentCodeNodesUnavailable,
		Reason: reason,
	}
}

// Release 释放租约；附件上传本身由终端直连节点，释放只清理本地粘性映射。
func (r *RemoteAttachment) Release(_ context.Context, leaseID string) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	binding, ok := r.leases[leaseID]
	if ok {
		delete(r.leases, leaseID)
		if sticky, found := r.sticky[binding.key]; found && sticky.endpoint == binding.endpoint {
			delete(r.sticky, binding.key)
		}
	}
	return nil
}

type nodeAttachmentStatus struct {
	index  int
	node   AttachmentNode
	status attachmentStatus
	err    error
}

type attachmentStatus struct {
	NodeID        string `json:"nodeId"`
	Active        int    `json:"active"`
	MaxConcurrent int    `json:"maxConcurrent"`
	Available     int    `json:"available"`
	AdvertiseIP   string `json:"advertiseIp"`
	TCPPort       uint16 `json:"tcpPort"`
	UDPPort       uint16 `json:"udpPort"`
}

func (s attachmentStatus) endpoint(node AttachmentNode) contracts.AttachmentEndpoint {
	ip := s.AdvertiseIP
	if ip == "" {
		if parsed, err := url.Parse(node.URL); err == nil {
			ip = parsed.Hostname()
		}
	}
	return contracts.AttachmentEndpoint{IP: ip, TCPPort: s.TCPPort, UDPPort: s.UDPPort}
}

func (r *RemoteAttachment) fetchStatuses(ctx context.Context) []nodeAttachmentStatus {
	out := make([]nodeAttachmentStatus, 0, len(r.nodes))
	for index, node := range r.nodes {
		status, err := r.fetchStatus(ctx, node)
		out = append(out, nodeAttachmentStatus{index: index, node: node, status: status, err: err})
	}
	return out
}

func (r *RemoteAttachment) fetchStatus(ctx context.Context, node AttachmentNode) (attachmentStatus, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultAttachmentNodeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(callCtx, http.MethodGet, node.URL+"/internal/attachment/status", nil)
	if err != nil {
		return attachmentStatus{}, err
	}
	response, err := r.client.Do(request)
	if err != nil {
		return attachmentStatus{}, err
	}
	defer response.Body.Close()
	var parsed attachmentStatus
	if response.StatusCode != http.StatusOK {
		return attachmentStatus{}, fmt.Errorf("attachment node returned %d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(&parsed); err != nil {
		return attachmentStatus{}, fmt.Errorf("decode attachment status: %w", err)
	}
	return parsed, nil
}

func attachmentStatusAt(statuses []nodeAttachmentStatus, index int) (nodeAttachmentStatus, bool) {
	for _, status := range statuses {
		if status.index == index {
			return status, true
		}
	}
	return nodeAttachmentStatus{}, false
}

func (r *RemoteAttachment) remember(key, leaseID string, node int, endpoint contracts.AttachmentEndpoint, expiresAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	binding := attachmentBinding{node: node, key: key, endpoint: endpoint, expiresAt: expiresAt}
	r.sticky[key] = binding
	r.leases[leaseID] = binding
}

func (r *RemoteAttachment) stickyNode(key string) (int, bool) {
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

func (r *RemoteAttachment) dropSticky(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sticky, key)
}

func attachmentKey(phone, alarmID string) string {
	return phone + "/" + alarmID
}

var _ contracts.AttachmentAllocator = (*RemoteAttachment)(nil)
