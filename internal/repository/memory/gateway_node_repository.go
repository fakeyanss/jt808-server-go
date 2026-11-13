package memory

import (
	"context"
	"sync"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

const memoryNodeTTL = 30 * time.Second

// GatewayNodeRepository 是进程内节点注册表，仅用于单机与测试。
// 集群生产路径使用 SQL 后端，由数据库时间裁决过期。
type GatewayNodeRepository struct {
	mu    sync.Mutex
	nodes map[string]repository.GatewayNode
}

// NewGatewayNodeRepository 创建内存节点注册表。
func NewGatewayNodeRepository() *GatewayNodeRepository {
	return &GatewayNodeRepository{nodes: make(map[string]repository.GatewayNode)}
}

// Register 覆盖当前实例。内存后端不联动会话目录：内存目录没有节点信息，
// Router 恒返回未命中；集群注册与目录清理走 SQL 后端。
func (r *GatewayNodeRepository) Register(_ context.Context, gatewayID, instanceID, downlinkURL string) error {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes[gatewayID] = repository.GatewayNode{
		GatewayID:   gatewayID,
		InstanceID:  instanceID,
		DownlinkURL: downlinkURL,
		Status:      "online",
		StartedAt:   now,
		LastSeenAt:  now,
		ExpiresAt:   now.Add(memoryNodeTTL),
	}
	return nil
}
func (r *GatewayNodeRepository) Heartbeat(_ context.Context, gatewayID, instanceID string) (bool, error) {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[gatewayID]
	if !ok || node.InstanceID != instanceID {
		return false, nil
	}
	node.Status = "online"
	node.LastSeenAt = now
	node.ExpiresAt = now.Add(memoryNodeTTL)
	r.nodes[gatewayID] = node
	return true, nil
}

func (r *GatewayNodeRepository) Deregister(_ context.Context, gatewayID, instanceID string) error {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[gatewayID]
	if !ok || node.InstanceID != instanceID {
		return nil
	}
	node.Status = "offline"
	node.LastSeenAt = now
	node.ExpiresAt = now
	r.nodes[gatewayID] = node
	return nil
}

func (r *GatewayNodeRepository) Get(_ context.Context, gatewayID string) (*repository.GatewayNode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[gatewayID]
	if !ok {
		return nil, nil
	}
	copy := node
	return &copy, nil
}

func (r *GatewayNodeRepository) Stats(_ context.Context) (repository.GatewayNodeStats, error) {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	var stats repository.GatewayNodeStats
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
