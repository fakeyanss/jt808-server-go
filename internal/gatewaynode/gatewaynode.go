// Package gatewaynode 实现网关节点向 control 的注册、心跳与注销。
package gatewaynode

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	// HeartbeatInterval 是网关心跳间隔。
	HeartbeatInterval = 10 * time.Second
	// NodeTTL 是节点判死阈值：3 次心跳未上报。
	NodeTTL = 30 * time.Second

	PathRegister   = "/internal/v1/gateway/register"
	PathHeartbeat  = "/internal/v1/gateway/heartbeat"
	PathDeregister = "/internal/v1/gateway/deregister"
	PathRelease    = "/internal/v1/session/release"
)

// RegisterRequest 是网关注册请求。
type RegisterRequest struct {
	GatewayID   string `json:"gatewayId"`
	InstanceID  string `json:"instanceId"`
	DownlinkURL string `json:"downlinkUrl"`
}

// HeartbeatRequest 是网关心跳请求。
type HeartbeatRequest struct {
	GatewayID  string `json:"gatewayId"`
	InstanceID string `json:"instanceId"`
}

// ReleaseRequest 是终端断开时网关上报的会话释放请求。
// generation 必须与 claim 时一致，避免旧连接删掉新归属。
type ReleaseRequest struct {
	GatewayID  string `json:"gatewayId"`
	InstanceID string `json:"instanceId"`
	Phone      string `json:"phone"`
	SessionID  string `json:"sessionId"`
	Generation uint64 `json:"generation"`
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
		panic(fmt.Sprintf("gatewaynode: generate instance id: %v", err))
	}
	return hex.EncodeToString(buf[:])
}
