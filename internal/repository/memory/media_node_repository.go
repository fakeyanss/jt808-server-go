package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// MediaNodeRepository 是进程内媒体节点注册表，仅用于单机与测试。
// 集群生产路径使用 SQL 后端；两级后端都以调用方传入的时刻裁决过期与额度。
type MediaNodeRepository struct {
	mu    sync.Mutex
	nodes map[string]repository.MediaNode
}

// NewMediaNodeRepository 创建内存媒体节点注册表。
func NewMediaNodeRepository() *MediaNodeRepository {
	return &MediaNodeRepository{nodes: make(map[string]repository.MediaNode)}
}

func (r *MediaNodeRepository) Register(_ context.Context, node *repository.MediaNode, now time.Time) error {
	if node == nil || node.NodeID == "" || node.InstanceID == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *node
	copy.StartedAt = now
	copy.LastSeenAt = now
	copy.ExpiresAt = now.Add(repository.MediaNodeTTL)
	copy.Status = "online"
	// 新实例注册即顶替旧实例：旧进程的端点占用不再有意义。
	copy.EndpointsUsed = 0
	r.nodes[copy.NodeID] = copy
	*node = copy
	return nil
}

func (r *MediaNodeRepository) Heartbeat(_ context.Context, nodeID, instanceID string, load repository.MediaNodeLoad, now time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[nodeID]
	if !ok || node.InstanceID != instanceID {
		return false, nil
	}
	node.Status = "online"
	node.LastSeenAt = now
	node.ExpiresAt = now.Add(repository.MediaNodeTTL)
	node.ActiveChannels = load.ActiveChannels
	node.ActiveTCPConns = load.ActiveTCPConns
	node.InboundBitrateKbps = load.InboundBitrateKbps
	node.CPUPercent = load.CPUPercent
	node.MemoryBytes = load.MemoryBytes
	r.nodes[nodeID] = node
	return true, nil
}

func (r *MediaNodeRepository) Deregister(_ context.Context, nodeID, instanceID string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[nodeID]
	if !ok || node.InstanceID != instanceID {
		return nil
	}
	node.Status = "offline"
	node.LastSeenAt = now
	node.ExpiresAt = now
	r.nodes[nodeID] = node
	return nil
}

func (r *MediaNodeRepository) Get(_ context.Context, nodeID string) (*repository.MediaNode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[nodeID]
	if !ok {
		return nil, nil
	}
	copy := node
	return &copy, nil
}

func (r *MediaNodeRepository) ListOnline(_ context.Context, now time.Time) ([]*repository.MediaNode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*repository.MediaNode, 0, len(r.nodes))
	for _, node := range r.nodes {
		if node.Status != "online" || !node.ExpiresAt.After(now) {
			continue
		}
		copy := node
		out = append(out, &copy)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out, nil
}

func (r *MediaNodeRepository) Reserve(_ context.Context, nodeID, instanceID string, slots int64, now time.Time) (bool, error) {
	if slots <= 0 {
		slots = 1
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[nodeID]
	if !ok || node.InstanceID != instanceID || node.Status != "online" || !node.ExpiresAt.After(now) {
		return false, nil
	}
	if node.EndpointCapacity > 0 && node.EndpointsUsed+slots > node.EndpointCapacity {
		return false, nil
	}
	node.EndpointsUsed += slots
	r.nodes[nodeID] = node
	return true, nil
}

func (r *MediaNodeRepository) Release(_ context.Context, nodeID, instanceID string, slots int64, _ time.Time) error {
	if slots <= 0 {
		slots = 1
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[nodeID]
	if !ok || node.InstanceID != instanceID {
		return nil
	}
	if node.EndpointsUsed > slots {
		node.EndpointsUsed -= slots
	} else {
		node.EndpointsUsed = 0
	}
	r.nodes[nodeID] = node
	return nil
}

func (r *MediaNodeRepository) Stats(_ context.Context, now time.Time) (repository.MediaNodeStats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var stats repository.MediaNodeStats
	for _, node := range r.nodes {
		switch {
		case node.Status == "offline":
			stats.Offline++
		case node.ExpiresAt.After(now):
			stats.Online++
		default:
			stats.Expired++
		}
	}
	return stats, nil
}

var _ repository.MediaNodeRepository = (*MediaNodeRepository)(nil)
