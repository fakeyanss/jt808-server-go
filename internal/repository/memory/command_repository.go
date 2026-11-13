package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/clock"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// memoryCommandRepository 是 CommandRepository 的内存实现。
type memoryCommandRepository struct {
	mu      sync.RWMutex
	byID    map[string]*repository.DownlinkCommand
	byIdem  map[commandIdempotencyKey]string
	routing map[commandRoutingKey]string
	// nextSerial 是 per-phone 的流水号游标，只在 outbox 启用时使用。
	nextSerial map[string]uint16
	// clock 只用于记录用途的 updated_at/created_at，判定时刻由调用方传入。
	clock clock.Clock
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
func NewCommandRepository() repository.CommandRepository {
	return NewCommandRepositoryWithClock(clock.System())
}

// NewCommandRepositoryWithClock 用指定时钟创建内存仓储，供测试推进时间。
func NewCommandRepositoryWithClock(clk clock.Clock) repository.CommandRepository {
	if clk == nil {
		clk = clock.System()
	}
	return &memoryCommandRepository{
		byID:       map[string]*repository.DownlinkCommand{},
		byIdem:     map[commandIdempotencyKey]string{},
		routing:    map[commandRoutingKey]string{},
		nextSerial: map[string]uint16{},
		clock:      clk,
	}
}

func (r *memoryCommandRepository) Enqueue(_ context.Context, command *repository.DownlinkCommand) (*repository.DownlinkCommand, bool, error) {
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
	now := r.clock.Now()
	stored := cloneCommand(command)
	stored.Status = repository.CommandStatusPending
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

func (r *memoryCommandRepository) Get(_ context.Context, commandID string) (*repository.DownlinkCommand, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stored := r.byID[commandID]
	if stored == nil {
		return nil, repository.ErrCommandNotFound
	}
	return cloneCommand(stored), nil
}

func (r *memoryCommandRepository) FindByRouting(_ context.Context, phone string, msgID, serial uint16) (*repository.DownlinkCommand, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.routing[commandRoutingKey{phone: phone, msgID: msgID, serial: serial}]
	if !ok {
		return nil, repository.ErrCommandNotFound
	}
	stored := r.byID[id]
	if stored == nil {
		return nil, repository.ErrCommandNotFound
	}
	return cloneCommand(stored), nil
}

func (r *memoryCommandRepository) ListPending(_ context.Context, query repository.PendingQuery) ([]*repository.DownlinkCommand, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	commands := make([]*repository.DownlinkCommand, 0, len(r.byID))
	for _, stored := range r.byID {
		if !isQueueable(stored, query.DueBefore) {
			continue
		}
		commands = append(commands, cloneCommand(stored))
	}
	sortCommands(commands)
	return pageCommands(commands, query.Offset, query.Limit), nil
}

func (r *memoryCommandRepository) PendingStats(_ context.Context, now time.Time) (repository.PendingStats, error) {
	if now.IsZero() {
		now = r.clock.Now()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var stats repository.PendingStats
	for _, stored := range r.byID {
		if stored.Status != repository.CommandStatusPending {
			continue
		}
		stats.Pending++
		if !stored.ExpiresAt.IsZero() && !stored.ExpiresAt.After(now) {
			stats.Expired++
		}
	}
	return stats, nil
}

func (r *memoryCommandRepository) ClaimPending(_ context.Context, claim repository.CommandClaim) ([]*repository.DownlinkCommand, error) {
	if claim.Owner == "" {
		return nil, fmt.Errorf("claim commands: empty owner")
	}
	if claim.Lease <= 0 {
		return nil, fmt.Errorf("claim commands: lease must be positive")
	}
	now := claim.Now
	if now.IsZero() {
		now = r.clock.Now()
	}
	tolerance := claim.Tolerance
	if tolerance < 0 {
		tolerance = 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	candidates := make([]*repository.DownlinkCommand, 0, len(r.byID))
	for _, stored := range r.byID {
		if !isClaimable(stored, now, tolerance) {
			continue
		}
		candidates = append(candidates, stored)
	}
	sortCommands(candidates)
	if claim.Limit > 0 && len(candidates) > claim.Limit {
		candidates = candidates[:claim.Limit]
	}
	claimed := make([]*repository.DownlinkCommand, 0, len(candidates))
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
	if stored == nil || stored.Status != repository.CommandStatusPending || stored.LeaseOwner != owner {
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
	if stored == nil || stored.Status != repository.CommandStatusPending || stored.LeaseOwner != owner {
		return false, nil
	}
	stored.LeaseOwner = ""
	stored.LeaseExpiresAt = time.Time{}
	stored.NextAttemptAt = next
	stored.LastError = reason
	stored.UpdatedAt = r.clock.Now()
	return true, nil
}

func (r *memoryCommandRepository) ReleaseOffline(_ context.Context, commandID, owner string, attempts int, lastAttemptAt time.Time, next time.Time, reason string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := r.byID[commandID]
	if stored == nil || stored.Status != repository.CommandStatusPending || stored.LeaseOwner != owner {
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
	stored.UpdatedAt = r.clock.Now()
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
		if stored.Status == repository.CommandStatusPending && stored.Phone == phone &&
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
	if stored == nil || stored.Status != repository.CommandStatusPending {
		return false, nil
	}
	stored.LeaseOwner = ""
	stored.LeaseExpiresAt = time.Time{}
	stored.NextAttemptAt = next
	stored.LastError = reason
	stored.UpdatedAt = r.clock.Now()
	return true, nil
}

func (r *memoryCommandRepository) ExpirePending(_ context.Context, now time.Time, reason string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	expired := 0
	for _, stored := range r.byID {
		if stored.Status != repository.CommandStatusPending || stored.ExpiresAt.IsZero() || stored.ExpiresAt.After(now) {
			continue
		}
		stored.Status = repository.CommandStatusFailed
		stored.LastError = reason
		stored.LeaseOwner = ""
		stored.LeaseExpiresAt = time.Time{}
		stored.UpdatedAt = now
		expired++
	}
	return expired, nil
}

func (r *memoryCommandRepository) MarkAcked(_ context.Context, phone string, msgID, serial uint16) (bool, error) {
	return r.finish(phone, msgID, serial, repository.CommandStatusAcked, "")
}

func (r *memoryCommandRepository) MarkFailed(_ context.Context, phone string, msgID, serial uint16, reason string) (bool, error) {
	return r.finish(phone, msgID, serial, repository.CommandStatusFailed, reason)
}

func (r *memoryCommandRepository) finish(phone string, msgID, serial uint16, status repository.CommandStatus, reason string) (bool, error) {
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
	if stored.Status != repository.CommandStatusPending {
		// 已被并发推进为终态：保留 routing 索引供迟到/重复应答分类。
		return false, nil
	}
	stored.Status = status
	stored.LastError = reason
	stored.LeaseOwner = ""
	stored.LeaseExpiresAt = time.Time{}
	stored.UpdatedAt = r.clock.Now()
	// 保留 routing 索引：迟到/重复应答仍需按 (phone, msgID, serial) 找到终态记录，
	// 才能区分 duplicate 与 unmatched，而不是把迟到应答误报为无匹配。
	return true, nil
}

// isQueueable 判断命令是否属于 pending、已到可投时刻且未过期（用于 ListPending）。
func isQueueable(c *repository.DownlinkCommand, dueBefore time.Time) bool {
	if c.Status != repository.CommandStatusPending {
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

// isClaimable 判断命令是否可被租约占用：pending、已到可投时刻、未过期，
// 且原租约已超过「到期 + 容差」。容差让接管偏向保守，避免时钟偏差导致双持有。
func isClaimable(c *repository.DownlinkCommand, now time.Time, tolerance time.Duration) bool {
	if c.Status != repository.CommandStatusPending {
		return false
	}
	if !c.NextAttemptAt.IsZero() && c.NextAttemptAt.After(now) {
		return false
	}
	if !c.ExpiresAt.IsZero() && !c.ExpiresAt.After(now) {
		return false
	}
	if !c.LeaseExpiresAt.IsZero() && c.LeaseExpiresAt.Add(tolerance).After(now) {
		return false
	}
	return true
}

// sortCommands 以「可投时间、创建时间、命令 ID」稳定排序，保证分页结果可复现。
func sortCommands(commands []*repository.DownlinkCommand) {
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

func pageCommands(commands []*repository.DownlinkCommand, offset, limit int) []*repository.DownlinkCommand {
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
func cloneCommand(command *repository.DownlinkCommand) *repository.DownlinkCommand {
	if command == nil {
		return nil
	}
	copy := *command
	copy.Payload = append([]byte(nil), command.Payload...)
	return &copy
}

var _ repository.CommandRepository = (*memoryCommandRepository)(nil)
