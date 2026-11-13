package repository

import (
	"context"
	"errors"
	"time"
)

// ErrCommandNotFound 表示按 command_id 查询下行命令时无记录。
var ErrCommandNotFound = errors.New("command not found")

// CommandStatus 描述下行命令在 outbox 中的投递状态。
type CommandStatus string

const (
	// CommandStatusPending 表示命令已受理，尚未收到终端应答或仍在重试。
	CommandStatusPending CommandStatus = "pending"
	// CommandStatusAcked 表示终端已按协议应答。
	CommandStatusAcked CommandStatus = "acked"
	// CommandStatusFailed 表示重传耗尽、写失败、过期或会话关闭，投递失败。
	CommandStatusFailed CommandStatus = "failed"
)

// DownlinkCommand 是一条进入 outbox 的下行命令。
// Payload 保存消息体编码（不含报文头），用于后续重投与审计；
// Serial 为实际下发时使用的报文流水号，应答按 (Phone, MsgID, Serial) 回填状态。
//
// 重投时复用原始流水号：outbox 与终端应答仅以 (Phone, MsgID, Serial) 关联，
// 重新分配流水号会让已经到达的应答找不到对应记录，也会让同一命令的多次尝试
// 无法归并到一条记录上。复用带来的代价是 at-least-once 语义——终端可能重复
// 执行同一条命令，这由业务侧按幂等处理，不在 outbox 内消除。
type DownlinkCommand struct {
	CommandID      string        `json:"commandId"`
	IdempotencyKey string        `json:"idempotencyKey,omitempty"`
	Phone          string        `json:"phone"`
	MsgID          uint16        `json:"msgId"`
	Serial         uint16        `json:"serial"`
	Payload        []byte        `json:"-"`
	Status         CommandStatus `json:"status"`
	Attempts       int           `json:"attempts"`
	LastError      string        `json:"lastError,omitempty"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`

	// LastAttemptAt 是最近一次实际下发的时刻，零值表示尚未重投过。
	LastAttemptAt time.Time `json:"lastAttemptAt,omitempty"`
	// NextAttemptAt 是允许重投的最早时刻，零值等价于立即可投。
	NextAttemptAt time.Time `json:"nextAttemptAt,omitempty"`
	// ExpiresAt 是命令的过期时刻，零值表示不设过期；过期后由重投服务转 failed。
	ExpiresAt time.Time `json:"expiresAt,omitempty"`

	// LeaseOwner/LeaseExpiresAt 是并发保护：只有租约持有者可以下发该命令。
	// 租约到期后其它实例可重新占用，因此重投语义是 at-least-once。
	LeaseOwner     string    `json:"leaseOwner,omitempty"`
	LeaseExpiresAt time.Time `json:"leaseExpiresAt,omitempty"`
}

// PendingQuery 描述待投命令的分页查询条件。
// 可投时刻与过期判定由仓储用数据库当前时间完成，调用方不传判定时刻。
type PendingQuery struct {
	// Offset/Limit 用于分页，Limit<=0 表示不限制条数（仅供运维排查使用）。
	Offset int
	Limit  int
}

// CommandSchedule 是入队时的相对时间参数，全部相对数据库当前时间。
type CommandSchedule struct {
	// InitialWait 是首次允许投递的延迟；<=0 表示立即可投。
	InitialWait time.Duration
	// ExpireAfter 是过期时长；0 表示不设过期，负数表示入队即已过期。
	ExpireAfter time.Duration
}

// PendingStats 是 outbox 的 pending 水位。
type PendingStats struct {
	Pending int
	Expired int
}

// CommandClaim 描述一次租约占用的条件。
type CommandClaim struct {
	// Owner 是本实例的租约标识，必须非空且跨实例唯一。
	Owner string
	// Limit 是本批最多占用的条数。
	Limit int
	// Lease 是租约时长；必须覆盖一次下发到应答返回的窗口。
	Lease time.Duration
}

// CommandRepository 下行命令 outbox 仓储。
// Enqueue 以 (Phone, IdempotencyKey) 保证幂等：重复键返回已有记录且 created=false。
type CommandRepository interface {
	// Enqueue 以 (Phone, IdempotencyKey) 保证幂等：重复键返回已有记录且 created=false。
	// next_attempt_at/expires_at 由 schedule 相对数据库当前时间计算。
	Enqueue(ctx context.Context, command *DownlinkCommand, schedule CommandSchedule) (stored *DownlinkCommand, created bool, err error)
	Get(ctx context.Context, commandID string) (*DownlinkCommand, error)
	// FindByRouting 按 (Phone, MsgID, Serial) 反查命令，用于处理终端应答与失败事件。
	FindByRouting(ctx context.Context, phone string, msgID, serial uint16) (*DownlinkCommand, error)
	// ListPending 按可投时间分页扫描 pending 命令，不改变租约与状态。
	ListPending(ctx context.Context, query PendingQuery) ([]*DownlinkCommand, error)
	// PendingStats 统计 pending 总量与其中已过期的数量。
	// 过期判定在 SQL 内用数据库时间完成，供运维探针使用。
	PendingStats(ctx context.Context) (PendingStats, error)
	// ClaimPending 原子占用一批可投命令，同一命令在租约内只会被一个实例占用。
	ClaimPending(ctx context.Context, claim CommandClaim) ([]*DownlinkCommand, error)
	// BeginAttempt 记录一次实际下发尝试（attempts+1、last_attempt_at），仅租约持有者有效。
	BeginAttempt(ctx context.Context, commandID, owner string) (bool, error)
	// ReleaseAttempt 释放租约并安排下一次可投时刻，用于真实下发失败（传输错误、网关 rejected）
	// 后的退避。attempts 已由 BeginAttempt 递增，这里只写退避延迟与 last_error。
	ReleaseAttempt(ctx context.Context, commandID, owner string, delay time.Duration, reason string) (bool, error)
	// ReleaseOffline 释放租约但保持 pending，用于设备离线或档案未就绪等“未真正下发”的场景：
	// attempts 与 last_attempt_at 回退到调用前的快照，不消耗也不清零重投次数；
	// delay 是相对数据库当前时间的短退避，避免设备离线期间每个 poll tick 空转。
	ReleaseOffline(ctx context.Context, commandID, owner string, attempts int, lastAttemptAt time.Time, delay time.Duration, reason string) (bool, error)
	// ScheduleRetry 按路由键安排重投，不要求租约归属；用于异步失败事件回填。
	ScheduleRetry(ctx context.Context, phone string, msgID, serial uint16, delay time.Duration, reason string) (bool, error)
	// ExpirePending 把已过期且仍为 pending 的命令转 failed，返回处理条数。
	ExpirePending(ctx context.Context, reason string) (int, error)
	// ExpireByRouting 把指定路由已过期且仍 pending 的命令转 failed，返回是否命中。
	// 过期判定在 SQL 内用数据库时间完成，供迟到的终端应答使用。
	ExpireByRouting(ctx context.Context, phone string, msgID, serial uint16, reason string) (bool, error)
	MarkAcked(ctx context.Context, phone string, msgID, serial uint16) (bool, error)
	MarkFailed(ctx context.Context, phone string, msgID, serial uint16, reason string) (bool, error)
	// NextSerial 为 (phone, msgID) 分配下一个下行流水号。
	// 实现必须在 1..65535 间循环、跳过 0，并跳过该 phone 下同 msgID 仍在途（pending）
	// 的流水号：回绕后复用同一 serial 只有在旧命令已终结时才安全。
	NextSerial(ctx context.Context, phone string, msgID uint16) (uint16, error)
}
