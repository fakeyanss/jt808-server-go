package repository

import (
	"context"
	"time"
)

// MediaNodeTTL 是媒体节点心跳的有效期。
//
// 与 gateway_nodes / attachment_nodes 同一口径：3 次心跳未续期即判失效，
// 过期节点不再参与 0x9101/0x9201 的新分配。
const MediaNodeTTL = 30 * time.Second

// MediaNodeLoad 是心跳携带的轻量负载指标。
//
// 只放小字段：分配决策要用它们，但判死的时效不能被心跳体拖慢。
// CPU 与内存允许缺省（nil）：采集不到的平台少报这一项，心跳照常成功。
type MediaNodeLoad struct {
	ActiveChannels     int64
	ActiveTCPConns     int64
	InboundBitrateKbps int64
	CPUPercent         *float64
	MemoryBytes        *uint64
}

// MediaNode 是媒体节点在共享库中的注册状态与当前负载。
//
// 地址分两类：
//   - InternalURL / PlaybackBaseURL 是 control 与播放侧可达的地址；
//   - PushIP / PushTCPPort / PushUDPPort 是终端可达的推流地址，
//     NAT 场景下由媒体节点用 advertiseIp 填写。
//
// EndpointCapacity / EndpointsUsed 是端点（并发流）池的口径：
// 容量来自节点配置，已用量由 control 在原子占位与释放时维护。
type MediaNode struct {
	NodeID     string
	InstanceID string

	InternalURL     string
	PushIP          string
	PushTCPPort     uint16
	PushUDPPort     uint16
	PlaybackBaseURL string

	EndpointCapacity   int64
	EndpointsUsed      int64
	MaxBitrateKbps     int64
	InboundBitrateKbps int64
	ActiveChannels     int64
	ActiveTCPConns     int64
	CPUPercent         *float64
	MemoryBytes        *uint64

	Status     string
	StartedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

// MediaNodeStats 是媒体节点存活水位。
type MediaNodeStats struct {
	Online  int64
	Offline int64
	Expired int64
}

// MediaNodeRepository 维护媒体节点的注册、心跳、注销与端点占位。
//
// 所有时刻由调用方从进程时钟取得后传入，仓储不自行取时间；
// Heartbeat 返回 false 表示 instance_id 已被新实例顶替或节点不存在，
// 调用方应重新注册而不是继续使用旧负载。
type MediaNodeRepository interface {
	// Register 覆盖同 node_id 的旧实例（含旧 instance_id 与旧端点占用），
	// 避免重启后 control 继续向已消失的实例分配端点。
	Register(ctx context.Context, node *MediaNode, now time.Time) error
	// Heartbeat 刷新存活与负载；只更新小指标，不覆盖 control 维护的端点数。
	Heartbeat(ctx context.Context, nodeID, instanceID string, load MediaNodeLoad, now time.Time) (bool, error)
	Deregister(ctx context.Context, nodeID, instanceID string, now time.Time) error
	Get(ctx context.Context, nodeID string) (*MediaNode, error)
	// ListOnline 返回在线且未过期的节点，按 node_id 排序。
	ListOnline(ctx context.Context, now time.Time) ([]*MediaNode, error)
	// Reserve 原子占用端点：仅在节点在线、未过期且仍有额度时成功。
	// 并发分配同一“当前最低”节点时，只有一个调用方能占到最后一个额度。
	Reserve(ctx context.Context, nodeID, instanceID string, slots int64, now time.Time) (bool, error)
	// Release 归还端点；已用量不会被减成负数。
	Release(ctx context.Context, nodeID, instanceID string, slots int64, now time.Time) error
	Stats(ctx context.Context, now time.Time) (MediaNodeStats, error)
}

// MediaNodeHasCapacity 判断节点是否还能接受新端点。
//
// 容量有两个口径，必须同时满足：
//   - 端点池：已用 < 容量（容量 <=0 视为不限）；
//   - 入流码率：未达到 max_bitrate_kbps（未配置上限视为不限）。
func MediaNodeHasCapacity(node *MediaNode) bool {
	if node == nil {
		return false
	}
	if node.EndpointCapacity > 0 && node.EndpointsUsed >= node.EndpointCapacity {
		return false
	}
	if node.MaxBitrateKbps > 0 && node.InboundBitrateKbps >= node.MaxBitrateKbps {
		return false
	}
	return true
}
