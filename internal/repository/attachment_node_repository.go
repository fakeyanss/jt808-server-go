package repository

import (
	"context"
	"time"
)

// AttachmentNodeTTL 是附件节点心跳的有效期。
// 与网关节点同一口径：超时即判失效，0x9207 不再推送到该地址。
const AttachmentNodeTTL = 30 * time.Second

// AttachmentNode 是附件节点注册到 control 的可达地址。
type AttachmentNode struct {
	NodeID     string
	InstanceID string
	BaseURL    string
	Status     string
	StartedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

// AttachmentNodeStats 是附件节点存活水位。
type AttachmentNodeStats struct {
	Online  int64
	Offline int64
	Expired int64
}

// AttachmentNodeRepository 维护附件节点的注册、心跳与注销。
//
// Register 会覆盖同 node_id 的旧实例；Heartbeat 返回 false 表示 instance_id
// 已被新实例顶替或节点不存在，调用方应重新注册而不是继续使用旧地址。
type AttachmentNodeRepository interface {
	Register(ctx context.Context, nodeID, instanceID, baseURL string, now time.Time) error
	Heartbeat(ctx context.Context, nodeID, instanceID string, now time.Time) (bool, error)
	Deregister(ctx context.Context, nodeID, instanceID string, now time.Time) error
	Get(ctx context.Context, nodeID string) (*AttachmentNode, error)
	ListOnline(ctx context.Context, now time.Time) ([]*AttachmentNode, error)
	Stats(ctx context.Context, now time.Time) (AttachmentNodeStats, error)
}
