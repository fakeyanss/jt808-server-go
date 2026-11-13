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
