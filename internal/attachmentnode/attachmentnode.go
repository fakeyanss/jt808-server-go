// Package attachmentnode 实现附件节点向 control 的注册、心跳与注销。
//
// 附件节点不连数据库：注册地址与存活状态由 control 落库，
// control 据此把 0x9207 推送到正确的边缘节点。
package attachmentnode

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	// HeartbeatInterval 是附件节点心跳间隔。
	HeartbeatInterval = 10 * time.Second
	// NodeTTL 是节点判死阈值：3 次心跳未上报。
	NodeTTL = 30 * time.Second

	PathRegister   = "/internal/v1/attachment/nodes/register"
	PathHeartbeat  = "/internal/v1/attachment/nodes/heartbeat"
	PathDeregister = "/internal/v1/attachment/nodes/deregister"
	// PathFTPLogin 是附件节点校验 FTP 登录并取回任务配置的入口。
	PathFTPLogin = "/internal/v1/attachment/ftp/login"
	// PathFTPControl 是 control 推送到附件节点的 0x9207 控制入口。
	PathFTPControl = "/internal/attachment/ftp/control"
)

// RegisterRequest 是附件节点注册请求。
type RegisterRequest struct {
	NodeID     string `json:"nodeId"`
	InstanceID string `json:"instanceId"`
	BaseURL    string `json:"baseUrl"`
}

// HeartbeatRequest 是附件节点心跳请求。
type HeartbeatRequest struct {
	NodeID     string `json:"nodeId"`
	InstanceID string `json:"instanceId"`
}

// StatusResponse 是注册/心跳的通用响应。
type StatusResponse struct {
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// FTPLoginRequest 是附件节点替终端校验一次性账号的请求。
type FTPLoginRequest struct {
	NodeID   string `json:"nodeId"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// FTPLoginResponse 是登录成功后返回的任务配置。
type FTPLoginResponse struct {
	Phone      string `json:"phone"`
	Serial     uint16 `json:"serial"`
	UploadPath string `json:"uploadPath"`
	// MaxBytes 是单文件上限；0 表示不限制。
	MaxBytes int64 `json:"maxBytes"`
}

// FTPControlRequest 是 control 推送到附件节点的上传控制指令。
//
// Action 与 0x9207 同值：0 暂停、1 继续、2 取消；Seq 单调递增，节点据此去重。
type FTPControlRequest struct {
	Phone  string `json:"phone"`
	Serial uint16 `json:"serial"`
	Action uint8  `json:"action"`
	Seq    uint32 `json:"seq"`
}

// NewInstanceID 生成进程启动期唯一的实例标识；重启即变。
func NewInstanceID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(fmt.Sprintf("attachmentnode: generate instance id: %v", err))
	}
	return hex.EncodeToString(buf[:])
}
