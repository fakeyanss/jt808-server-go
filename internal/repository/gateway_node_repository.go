package repository

import (
	"context"
	"time"
)

// GatewayNode 是网关节点在共享库中的注册状态。
// InstanceID 在进程启动时生成，重启即变；同一 GatewayID 只允许一个当前实例。
type GatewayNode struct {
	GatewayID   string
	InstanceID  string
	DownlinkURL string
	Status      string
	StartedAt   time.Time
	LastSeenAt  time.Time
	ExpiresAt   time.Time
}

// GatewayNodeRepository 维护网关节点的注册、心跳与注销。
// 所有时间由数据库计算：Register/Heartbeat/Deregister 不接收应用侧时间，
// 过期判断由 SQL 完成；Heartbeat 返回 false 表示 gateway_id/instance_id
// 不匹配或节点不存在，调用方应重新注册而不是继续使用旧实例。
type GatewayNodeRepository interface {
	Register(ctx context.Context, gatewayID, instanceID, downlinkURL string) error
	Heartbeat(ctx context.Context, gatewayID, instanceID string) (bool, error)
	Deregister(ctx context.Context, gatewayID, instanceID string) error
	Get(ctx context.Context, gatewayID string) (*GatewayNode, error)
}
