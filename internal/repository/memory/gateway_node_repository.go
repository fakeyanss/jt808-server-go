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
