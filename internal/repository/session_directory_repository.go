package repository

import (
	"context"
	"time"
)

// SessionOwner 是一条会话在网关上的归属：节点、实例与本地会话。
// instance_id 在网关进程启动时生成，重启即变；session_id 是网关本地连接标识。
type SessionOwner struct {
	GatewayID  string
	InstanceID string
	SessionID  string
}

// SessionRecord 是 session_directory 的一行。
type SessionRecord struct {
	Phone string
	SessionOwner
	Generation uint64
	ClaimedAt  time.Time
}

// SessionDirectoryRepository 是管控侧的会话归属索引。
//
// 所有时间由数据库写入与裁决：Claim 不接收应用侧时间，claimed_at 由 SQL 生成。
// 目录不设 per-phone 有效期，会话是否有效由 gateway_nodes 的过期联表推出。
type SessionDirectoryRepository interface {
	// Claim 认领 phone 的会话归属。
	// 同一 owner 重复 claim 幂等：直接返回现有 generation，不递增；
	// gateway/instance/session 任一变化才 generation+1。
	Claim(ctx context.Context, phone string, owner SessionOwner) (uint64, error)
	// Release 删除归属行；条件必须同时匹配 owner 与 generation，
	// 避免旧连接删掉新归属。
	Release(ctx context.Context, phone string, owner SessionOwner, generation uint64) error
	// Validate 判断事件 generation 是否仍归属于该会话：
	// 行存在、owner 完全匹配且事件 generation 不低于目录值。
	Validate(ctx context.Context, phone string, owner SessionOwner, generation uint64) (bool, error)
	// Get 返回目录快照；不存在返回 nil。
	Get(ctx context.Context, phone string) (*SessionRecord, error)
	// Route 解析 phone 当前可用的下行入口。
	//
	// 判定全部在 SQL 内完成：目录行存在、所属节点 status=online 且
	// expires_at 晚于数据库当前时间，才返回该节点的 downlink_url。
	// sessionID 非空时还必须与目录一致，避免旧调用方把命令发到新会话上。
	// 查不到或节点不可用时返回 ok=false，由调用方按 offline 处理。
	Route(ctx context.Context, phone, sessionID string) (url string, ok bool, err error)
}
