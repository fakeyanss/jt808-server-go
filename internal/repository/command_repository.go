package repository

import (
	"context"
	"errors"
	"fmt"
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
	// CommandStatusFailed 表示重传耗尽、写失败或会话关闭，投递失败。
	CommandStatusFailed CommandStatus = "failed"
)

// DownlinkCommand 是一条进入 outbox 的下行命令。
// Payload 保存消息体编码（不含报文头），用于后续重投与审计；
// Serial 为实际下发时使用的报文流水号，应答按 (Phone, MsgID, Serial) 回填状态。
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
}

// CommandRepository 下行命令 outbox 仓储。
// Enqueue 以 (Phone, IdempotencyKey) 保证幂等：重复键返回已有记录且 created=false。
type CommandRepository interface {
	Enqueue(ctx context.Context, command *DownlinkCommand) (stored *DownlinkCommand, created bool, err error)
	Get(ctx context.Context, commandID string) (*DownlinkCommand, error)
	MarkAcked(ctx context.Context, phone string, msgID, serial uint16) (bool, error)
	MarkFailed(ctx context.Context, phone string, msgID, serial uint16, reason string) (bool, error)
}

// memoryCommandRepository 是 CommandRepository 的内存实现。
type memoryCommandRepository struct {
	mu      sync.RWMutex
	byID    map[string]*DownlinkCommand
	byIdem  map[commandIdempotencyKey]string
	routing map[commandRoutingKey]string
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
		byID:    map[string]*DownlinkCommand{},
		byIdem:  map[commandIdempotencyKey]string{},
		routing: map[commandRoutingKey]string{},
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
		stored.Attempts = 1
	}
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = now
	}
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
	if stored == nil || stored.Status != CommandStatusPending {
		delete(r.routing, key)
		return false, nil
	}
	stored.Status = status
	stored.LastError = reason
	stored.UpdatedAt = time.Now()
	delete(r.routing, key)
	return true, nil
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
