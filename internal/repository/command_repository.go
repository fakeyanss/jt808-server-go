package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
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
type PendingQuery struct {
	// DueBefore 只返回 NextAttemptAt 不晚于该时刻的命令；零值表示不限制。
	DueBefore time.Time
	// Offset/Limit 用于分页，Limit<=0 表示不限制条数（仅供运维排查使用）。
	Offset int
	Limit  int
}

// CommandClaim 描述一次租约占用的条件。
type CommandClaim struct {
	// Owner 是本实例的租约标识，必须非空且跨实例唯一。
	Owner string
	// Now 是判定可投与租约是否失效的时刻。
	Now time.Time
	// Limit 是本批最多占用的条数。
	Limit int
	// Lease 是租约时长；必须覆盖一次下发到应答返回的窗口。
	Lease time.Duration
}

// CommandRepository 下行命令 outbox 仓储。
// Enqueue 以 (Phone, IdempotencyKey) 保证幂等：重复键返回已有记录且 created=false。
type CommandRepository interface {
	Enqueue(ctx context.Context, command *DownlinkCommand) (stored *DownlinkCommand, created bool, err error)
	Get(ctx context.Context, commandID string) (*DownlinkCommand, error)
	// FindByRouting 按 (Phone, MsgID, Serial) 反查命令，用于处理终端应答与失败事件。
	FindByRouting(ctx context.Context, phone string, msgID, serial uint16) (*DownlinkCommand, error)
	// ListPending 按可投时间分页扫描 pending 命令，不改变租约与状态。
	ListPending(ctx context.Context, query PendingQuery) ([]*DownlinkCommand, error)
	// ClaimPending 原子占用一批可投命令，同一命令在租约内只会被一个实例占用。
	ClaimPending(ctx context.Context, claim CommandClaim) ([]*DownlinkCommand, error)
	// BeginAttempt 记录一次实际下发尝试（attempts+1、last_attempt_at），仅租约持有者有效。
	BeginAttempt(ctx context.Context, commandID, owner string, now time.Time) (bool, error)
	// ReleaseAttempt 释放租约并安排下一次可投时刻，用于真实下发失败（传输错误、网关 rejected）
	// 后的退避。attempts 已由 BeginAttempt 递增，这里只写退避时刻与 last_error。
	ReleaseAttempt(ctx context.Context, commandID, owner string, next time.Time, reason string) (bool, error)
	// ReleaseOffline 释放租约但保持 pending，用于设备离线或档案未就绪等“未真正下发”的场景：
	// attempts 与 last_attempt_at 回退到调用前的快照，不消耗也不清零重投次数；
	// next 写为短退避后的下一次可投时刻，避免设备离线期间每个 poll tick 空转。
	ReleaseOffline(ctx context.Context, commandID, owner string, attempts int, lastAttemptAt time.Time, next time.Time, reason string) (bool, error)
	// ScheduleRetry 按路由键安排重投，不要求租约归属；用于异步失败事件回填。
	ScheduleRetry(ctx context.Context, phone string, msgID, serial uint16, next time.Time, reason string) (bool, error)
	// ExpirePending 把已过期且仍为 pending 的命令转 failed，返回处理条数。
	ExpirePending(ctx context.Context, now time.Time, reason string) (int, error)
	MarkAcked(ctx context.Context, phone string, msgID, serial uint16) (bool, error)
	MarkFailed(ctx context.Context, phone string, msgID, serial uint16, reason string) (bool, error)
	// NextSerial 为 (phone, msgID) 分配下一个下行流水号。
	// 实现必须在 1..65535 间循环、跳过 0，并跳过该 phone 下同 msgID 仍在途（pending）
	// 的流水号：回绕后复用同一 serial 只有在旧命令已终结时才安全。
	NextSerial(ctx context.Context, phone string, msgID uint16) (uint16, error)
}

// memoryCommandRepository 是 CommandRepository 的内存实现。
type memoryCommandRepository struct {
	mu      sync.RWMutex
	byID    map[string]*DownlinkCommand
	byIdem  map[commandIdempotencyKey]string
	routing map[commandRoutingKey]string
	// nextSerial 是 per-phone 的流水号游标，只在 outbox 启用时使用。
	nextSerial map[string]uint16
}

type commandIdempotencyKey struct {
	phone string
	key   string
}

type commandRoutingKey struct {
	phone  string
	msgID  uint16
	serial uint16
}

// NewCommandRepository 创建独立的内存下行命令仓储。
func NewCommandRepository() CommandRepository {
	return &memoryCommandRepository{
		byID:       map[string]*DownlinkCommand{},
		byIdem:     map[commandIdempotencyKey]string{},
		routing:    map[commandRoutingKey]string{},
		nextSerial: map[string]uint16{},
	}
}

func (r *memoryCommandRepository) Enqueue(_ context.Context, command *DownlinkCommand) (*DownlinkCommand, bool, error) {
	if command == nil {
		return nil, false, fmt.Errorf("enqueue command: nil command")
	}
	if command.CommandID == "" {
		return nil, false, fmt.Errorf("enqueue command: empty command id")
	}
	if command.IdempotencyKey == "" {
		return nil, false, fmt.Errorf("enqueue command: empty idempotency key")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := commandIdempotencyKey{phone: command.Phone, key: command.IdempotencyKey}
	if id, ok := r.byIdem[key]; ok {
		return cloneCommand(r.byID[id]), false, nil
	}
	now := time.Now()
	stored := cloneCommand(command)
	stored.Status = CommandStatusPending
	if stored.Attempts == 0 {
		// 首次下发即第一次尝试，重投在此基础上累加。
		stored.Attempts = 1
	}
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = now
	}
	if stored.NextAttemptAt.IsZero() {
		stored.NextAttemptAt = now
	}
	stored.LeaseOwner = ""
	stored.LeaseExpiresAt = time.Time{}
	stored.UpdatedAt = now
	r.byID[stored.CommandID] = stored
	r.byIdem[key] = stored.CommandID
	r.routing[commandRoutingKey{phone: stored.Phone, msgID: stored.MsgID, serial: stored.Serial}] = stored.CommandID
	return cloneCommand(stored), true, nil
}

func (r *memoryCommandRepository) Get(_ context.Context, commandID string) (*DownlinkCommand, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stored := r.byID[commandID]
	if stored == nil {
		return nil, ErrCommandNotFound
	}
	return cloneCommand(stored), nil
}

func (r *memoryCommandRepository) FindByRouting(_ context.Context, phone string, msgID, serial uint16) (*DownlinkCommand, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.routing[commandRoutingKey{phone: phone, msgID: msgID, serial: serial}]
	if !ok {
		return nil, ErrCommandNotFound
	}
	stored := r.byID[id]
	if stored == nil {
		return nil, ErrCommandNotFound
	}
	return cloneCommand(stored), nil
}

func (r *memoryCommandRepository) ListPending(_ context.Context, query PendingQuery) ([]*DownlinkCommand, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	commands := make([]*DownlinkCommand, 0, len(r.byID))
	for _, stored := range r.byID {
		if !stored.isQueueable(query.DueBefore) {
			continue
		}
		commands = append(commands, cloneCommand(stored))
	}
	sortCommands(commands)
	return pageCommands(commands, query.Offset, query.Limit), nil
}

func (r *memoryCommandRepository) ClaimPending(_ context.Context, claim CommandClaim) ([]*DownlinkCommand, error) {
	if claim.Owner == "" {
		return nil, fmt.Errorf("claim commands: empty owner")
	}
	if claim.Lease <= 0 {
		return nil, fmt.Errorf("claim commands: lease must be positive")
	}
	now := claim.Now
	if now.IsZero() {
		now = time.Now()
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	candidates := make([]*DownlinkCommand, 0, len(r.byID))
	for _, stored := range r.byID {
		if !stored.isClaimable(now) {
			continue
		}
		candidates = append(candidates, stored)
	}
	sortCommands(candidates)
	if claim.Limit > 0 && len(candidates) > claim.Limit {
		candidates = candidates[:claim.Limit]
	}
	claimed := make([]*DownlinkCommand, 0, len(candidates))
	for _, stored := range candidates {
		stored.LeaseOwner = claim.Owner
		stored.LeaseExpiresAt = now.Add(claim.Lease)
		stored.UpdatedAt = now
		claimed = append(claimed, cloneCommand(stored))
	}
	return claimed, nil
}

func (r *memoryCommandRepository) BeginAttempt(_ context.Context, commandID, owner string, now time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := r.byID[commandID]
	if stored == nil || stored.Status != CommandStatusPending || stored.LeaseOwner != owner {
		return false, nil
	}
	if !stored.LeaseExpiresAt.IsZero() && !stored.LeaseExpiresAt.After(now) {
		return false, nil
	}
	stored.Attempts++
	stored.LastAttemptAt = now
	stored.UpdatedAt = now
	return true, nil
}

func (r *memoryCommandRepository) ReleaseAttempt(_ context.Context, commandID, owner string, next time.Time, reason string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := r.byID[commandID]
	if stored == nil || stored.Status != CommandStatusPending || stored.LeaseOwner != owner {
		return false, nil
	}
	stored.LeaseOwner = ""
	stored.LeaseExpiresAt = time.Time{}
	stored.NextAttemptAt = next
	stored.LastError = reason
	stored.UpdatedAt = time.Now()
	return true, nil
}

func (r *memoryCommandRepository) ReleaseOffline(_ context.Context, commandID, owner string, attempts int, lastAttemptAt time.Time, next time.Time, reason string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := r.byID[commandID]
	if stored == nil || stored.Status != CommandStatusPending || stored.LeaseOwner != owner {
		return false, nil
	}
	stored.LeaseOwner = ""
	stored.LeaseExpiresAt = time.Time{}
	// 离线不是一次真实下发：恢复占用前的尝试次数与时间，避免反复离线把
	// MaxAttempts 这道闸刷掉；同时写入短退避，避免每个 poll tick 都重新占用。
	stored.Attempts = attempts
	stored.LastAttemptAt = lastAttemptAt
	stored.NextAttemptAt = next
	stored.LastError = reason
	stored.UpdatedAt = time.Now()
	return true, nil
}

// NextSerial 在内存中原子分配流水号：per-phone 游标自增，跳过 0 与同 msgID 的在途组合。
func (r *memoryCommandRepository) NextSerial(_ context.Context, phone string, msgID uint16) (uint16, error) {
	if phone == "" {
		return 0, fmt.Errorf("next serial: empty phone")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next, ok := r.nextSerial[phone]
	if !ok || next == 0 {
		// 首次分配从该 phone 已存在的最大流水号之后开始，避免与重启前的记录绕回。
		for key := range r.routing {
			if key.phone == phone && key.serial > next {
				next = key.serial
			}
		}
		next = wrapSerial(next + 1)
	}
	for i := 0; i < 65535; i++ {
		if !r.serialInFlight(phone, msgID, next) {
			r.nextSerial[phone] = wrapSerial(next + 1)
			return next, nil
		}
		next = wrapSerial(next + 1)
	}
	return 0, fmt.Errorf("next serial: no free serial for %s/%04X", phone, msgID)
}

// serialInFlight 判断同一 phone 下同 msgID 的 serial 是否仍有 pending 命令。
func (r *memoryCommandRepository) serialInFlight(phone string, msgID, serial uint16) bool {
	for _, stored := range r.byID {
		if stored.Status == CommandStatusPending && stored.Phone == phone &&
			stored.MsgID == msgID && stored.Serial == serial {
			return true
		}
	}
	return false
}

// wrapSerial 在 1..65535 间循环，跳过协议保留的 0。
func wrapSerial(value uint16) uint16 {
	if value == 0 {
		return 1
	}
	return value
}

func (r *memoryCommandRepository) ScheduleRetry(_ context.Context, phone string, msgID, serial uint16, next time.Time, reason string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.routing[commandRoutingKey{phone: phone, msgID: msgID, serial: serial}]
	if !ok {
		return false, nil
	}
	stored := r.byID[id]
	if stored == nil || stored.Status != CommandStatusPending {
		return false, nil
	}
	stored.LeaseOwner = ""
	stored.LeaseExpiresAt = time.Time{}
	stored.NextAttemptAt = next
	stored.LastError = reason
	stored.UpdatedAt = time.Now()
	return true, nil
}

func (r *memoryCommandRepository) ExpirePending(_ context.Context, now time.Time, reason string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	expired := 0
	for _, stored := range r.byID {
		if stored.Status != CommandStatusPending || stored.ExpiresAt.IsZero() || stored.ExpiresAt.After(now) {
			continue
		}
		stored.Status = CommandStatusFailed
		stored.LastError = reason
		stored.LeaseOwner = ""
		stored.LeaseExpiresAt = time.Time{}
		stored.UpdatedAt = now
		expired++
	}
	return expired, nil
}

func (r *memoryCommandRepository) MarkAcked(_ context.Context, phone string, msgID, serial uint16) (bool, error) {
	return r.finish(phone, msgID, serial, CommandStatusAcked, "")
}

func (r *memoryCommandRepository) MarkFailed(_ context.Context, phone string, msgID, serial uint16, reason string) (bool, error) {
	return r.finish(phone, msgID, serial, CommandStatusFailed, reason)
}

func (r *memoryCommandRepository) finish(phone string, msgID, serial uint16, status CommandStatus, reason string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := commandRoutingKey{phone: phone, msgID: msgID, serial: serial}
	id, ok := r.routing[key]
	if !ok {
		return false, nil
	}
	stored := r.byID[id]
	if stored == nil {
		delete(r.routing, key)
		return false, nil
	}
	if stored.Status != CommandStatusPending {
		// 已被并发推进为终态：保留 routing 索引供迟到/重复应答分类。
		return false, nil
	}
	stored.Status = status
	stored.LastError = reason
	stored.LeaseOwner = ""
	stored.LeaseExpiresAt = time.Time{}
	stored.UpdatedAt = time.Now()
	// 保留 routing 索引：迟到/重复应答仍需按 (phone, msgID, serial) 找到终态记录，
	// 才能区分 duplicate 与 unmatched，而不是把迟到应答误报为无匹配。
	return true, nil
}

// isQueueable 判断命令是否属于 pending、已到可投时刻且未过期（用于 ListPending）。
func (c *DownlinkCommand) isQueueable(dueBefore time.Time) bool {
	if c.Status != CommandStatusPending {
		return false
	}
	if !dueBefore.IsZero() && c.NextAttemptAt.After(dueBefore) {
		return false
	}
	if !dueBefore.IsZero() && !c.ExpiresAt.IsZero() && !c.ExpiresAt.After(dueBefore) {
		return false
	}
	return true
}

// isClaimable 判断命令是否可被租约占用：pending、已到可投时刻、未过期、租约未生效。
func (c *DownlinkCommand) isClaimable(now time.Time) bool {
	if c.Status != CommandStatusPending {
		return false
	}
	if !c.NextAttemptAt.IsZero() && c.NextAttemptAt.After(now) {
		return false
	}
	if !c.ExpiresAt.IsZero() && !c.ExpiresAt.After(now) {
		return false
	}
	if !c.LeaseExpiresAt.IsZero() && c.LeaseExpiresAt.After(now) {
		return false
	}
	return true
}

// sortCommands 以「可投时间、创建时间、命令 ID」稳定排序，保证分页结果可复现。
func sortCommands(commands []*DownlinkCommand) {
	sort.Slice(commands, func(i, j int) bool {
		if !commands[i].NextAttemptAt.Equal(commands[j].NextAttemptAt) {
			return commands[i].NextAttemptAt.Before(commands[j].NextAttemptAt)
		}
		if !commands[i].CreatedAt.Equal(commands[j].CreatedAt) {
			return commands[i].CreatedAt.Before(commands[j].CreatedAt)
		}
		return commands[i].CommandID < commands[j].CommandID
	})
}

func pageCommands(commands []*DownlinkCommand, offset, limit int) []*DownlinkCommand {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(commands) {
		return nil
	}
	commands = commands[offset:]
	if limit > 0 && len(commands) > limit {
		commands = commands[:limit]
	}
	return commands
}

func cloneCommand(command *DownlinkCommand) *DownlinkCommand {
	if command == nil {
		return nil
	}
	copy := *command
	copy.Payload = append([]byte(nil), command.Payload...)
	return &copy
}

var _ CommandRepository = (*memoryCommandRepository)(nil)
