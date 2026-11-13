// Package medianode 实现媒体节点向 control 的注册、心跳与注销。
//
// 媒体节点不连数据库：地址、容量与负载由 control 落库，control 据此在
// 0x9101/0x9201 分配时挑选负载最低且有额度的节点。
package medianode

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	// HeartbeatInterval 是媒体节点心跳间隔。
	HeartbeatInterval = 10 * time.Second
	// NodeTTL 是节点判死阈值：3 次心跳未上报。
	NodeTTL = 30 * time.Second

	PathRegister   = "/internal/v1/media/nodes/register"
	PathHeartbeat  = "/internal/v1/media/nodes/heartbeat"
	PathDeregister = "/internal/v1/media/nodes/deregister"
)

// RegisterRequest 是媒体节点注册请求。
//
// 地址分两类：internalUrl/playbackBaseUrl 面向 control 与播放侧，
// pushIp/pushTcpPort/pushUdpPort 面向终端（NAT 场景由 advertiseIp 填写）。
type RegisterRequest struct {
	NodeID     string `json:"nodeId"`
	InstanceID string `json:"instanceId"`

	InternalURL     string `json:"internalUrl"`
	PushIP          string `json:"pushIp"`
	PushTCPPort     uint16 `json:"pushTcpPort"`
	PushUDPPort     uint16 `json:"pushUdpPort"`
	PlaybackBaseURL string `json:"playbackBaseUrl"`

	// EndpointCapacity 是并发端点（流）上限；0 表示不限。
	EndpointCapacity int64 `json:"endpointCapacity"`
	// MaxBitrateKbps 是入流码率容量；0 表示不限。
	MaxBitrateKbps int64 `json:"maxBitrateKbps"`
}

// Load 是心跳携带的轻量负载。
//
// 只放分配决策需要的小指标，避免心跳体变胖拖慢判死时效。
// 大体量明细（每路流、每个端口）必须另开上报通道，不得塞进这里。
type Load struct {
	ActiveChannels     int64 `json:"activeChannels"`
	ActiveTCPConns     int64 `json:"activeTcpConns"`
	InboundBitrateKbps int64 `json:"inboundBitrateKbps"`
	// CPUPercent 与 MemoryBytes 允许缺省：采集不到时不报，心跳照常成功。
	CPUPercent  *float64 `json:"cpuPercent,omitempty"`
	MemoryBytes *uint64  `json:"memoryBytes,omitempty"`
}

// HeartbeatRequest 是媒体节点心跳请求。
type HeartbeatRequest struct {
	NodeID     string `json:"nodeId"`
	InstanceID string `json:"instanceId"`
	Load       Load   `json:"load"`
}

// StatusResponse 是注册/心跳的通用响应。
type StatusResponse struct {
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// NewInstanceID 生成进程启动期唯一的实例标识；重启即变。
func NewInstanceID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(fmt.Sprintf("medianode: generate instance id: %v", err))
	}
	return hex.EncodeToString(buf[:])
}
