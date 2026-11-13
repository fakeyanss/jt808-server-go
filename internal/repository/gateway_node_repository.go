package repository

import (
	"context"
	"time"
)

// GatewayNodeTTL 是节点心跳的有效期：超过该时间未续期即视为失效。
// 由应用按进程时钟计算 expires_at，SQL 只比较调用方传入的时刻。
const GatewayNodeTTL = 30 * time.Second

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

// GatewayNodeStats 是节点存活状态的观测快照。
// Online 只统计 status=online 且未过期的节点；Expired 是仍在线但已超过
// 过期时间的节点，用于区分“进程停了但还没被清理”的残留。
type GatewayNodeStats struct {
	Online  int64
	Offline int64
	Expired int64
}

// GatewayNodeRepository 维护网关节点的注册、心跳与注销。
// 所有时刻由调用方从进程时钟取得后传入，仓储不自行取时间；
// Heartbeat 返回 false 表示 gateway_id/instance_id 不匹配或节点不存在，
// 调用方应重新注册而不是继续使用旧实例。
type GatewayNodeRepository interface {
	// Register 覆盖节点当前实例，并在同一事务内删除同 gateway_id 下
	// 旧 instance_id 的会话目录行，避免重启后归属抖动。
	Register(ctx context.Context, gatewayID, instanceID, downlinkURL string, now time.Time) error
	Heartbeat(ctx context.Context, gatewayID, instanceID string, now time.Time) (bool, error)
	Deregister(ctx context.Context, gatewayID, instanceID string, now time.Time) error
	Get(ctx context.Context, gatewayID string) (*GatewayNode, error)
	// Stats 返回节点存活统计；过期判定用调用方传入的时刻。
	Stats(ctx context.Context, now time.Time) (GatewayNodeStats, error)
}
