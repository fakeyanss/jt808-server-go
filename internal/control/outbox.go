// Package control 是内置 API 管控模块。
//
// 它拥有 durable outbox 的完整生命周期（幂等、租约、退避、过期、重投），
// 并通过 contracts 中的四类契约与 808 网关、媒体、附件交互。
// standalone 模式下所有契约由本包的 Local* 实现满足，不产生网络调用。
package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
)

// 重投相关的默认值。可由配置覆盖。
const (
	DefaultPollInterval  = 10 * time.Second
	DefaultBatchSize     = 50
	DefaultMaxAttempts   = 5
	DefaultLeaseDuration = time.Minute
	DefaultRetryBase     = 5 * time.Second
	DefaultRetryMax      = 5 * time.Minute
)

// OutboxOptions 是 outbox 的运行时参数。
type OutboxOptions struct {
	// Enabled 为 false 时不落库、不重投，下行直接发送（对应 off 模式）。
	Enabled       bool
	InitialWait   time.Duration
	ExpireAfter   time.Duration
	PollInterval  time.Duration
	BatchSize     int
	MaxAttempts   int
	LeaseDuration time.Duration
	RetryBase     time.Duration
	RetryMax      time.Duration
}

func (o OutboxOptions) withDefaults() OutboxOptions {
	if o.PollInterval <= 0 {
		o.PollInterval = DefaultPollInterval
	}
	if o.BatchSize <= 0 {
		o.BatchSize = DefaultBatchSize
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = DefaultMaxAttempts
	}
	if o.LeaseDuration <= 0 {
		o.LeaseDuration = DefaultLeaseDuration
	}
	if o.RetryBase <= 0 {
		o.RetryBase = DefaultRetryBase
	}
	if o.RetryMax <= 0 {
		o.RetryMax = DefaultRetryMax
	}
	if o.RetryMax < o.RetryBase {
		o.RetryMax = o.RetryBase
	}
	return o
}

// Sender 抽象一次带指定流水号的下行发送，便于测试注入。
type Sender func(sessionID string, message model.JT808Msg, serial uint16) error

// OutboxDependencies 是内置管控的外部依赖。
type OutboxDependencies struct {
	Commands repository.CommandRepository
	Devices  repository.DeviceRepository
	Conn     runtime.ConnectionRegistry
	Send     Sender
	// Owner 是租约标识；为空时按主机名 + 进程号 + 随机串生成。
	Owner string
	// Clock 便于测试控制时间；为空时使用 time.Now。
	Clock func() time.Time
}

// Outbox 是内置管控的 durable 下行投递模块。
type Outbox struct {
	commands repository.CommandRepository
	devices  repository.DeviceRepository
	conn     runtime.ConnectionRegistry
	send     Sender
	owner    string
	clock    func() time.Time
	opts     OutboxOptions
	// done 在 Run 退出后关闭，供关机流程等待当前 tick 收尾。
	done chan struct{}
}

// NewOutbox 校验依赖并创建 outbox。
// Enabled=false 时允许 Commands 为空，此时 Submit 直接发送、不落库。
func NewOutbox(deps OutboxDependencies, options OutboxOptions) (*Outbox, error) {
	options = options.withDefaults()
	if options.Enabled {
		if deps.Commands == nil {
			return nil, fmt.Errorf("outbox: command repository is required when enabled")
		}
		if deps.Devices == nil {
			return nil, fmt.Errorf("outbox: device repository is required when enabled")
		}
		if deps.Conn == nil {
			return nil, fmt.Errorf("outbox: connection registry is required when enabled")
		}
	}
	if deps.Send == nil {
		return nil, fmt.Errorf("outbox: sender is required")
	}
	owner := deps.Owner
	if owner == "" {
		owner = newLeaseOwner()
	}
	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Outbox{
		commands: deps.Commands,
		devices:  deps.Devices,
		conn:     deps.Conn,
		send:     deps.Send,
		owner:    owner,
		clock:    clock,
		opts:     options,
		done:     make(chan struct{}),
	}, nil
}

// Enabled 表示 outbox 是否启用（durable/memory）。
func (o *Outbox) Enabled() bool {
	return o != nil && o.opts.Enabled && o.commands != nil
}

// Done 在 Run 退出后关闭；未调用 Run 时永不关闭。
func (o *Outbox) Done() <-chan struct{} { return o.done }

// Submit 实现 contracts.Downlink：off 模式直接发送，其余模式先入队再发送。
// 返回 DownlinkResult 供 HTTP 层区分 202（新建）与 200（幂等命中）。
func (o *Outbox) Submit(ctx context.Context, request contracts.DownlinkRequest) (contracts.DownlinkResult, error) {
	if o == nil {
		return contracts.DownlinkResult{}, fmt.Errorf("outbox is not configured")
	}
	if !o.Enabled() {
		// off 模式保持既有行为：直接发送，不落库、不可查、不重投。
		_ = o.send(request.SessionID, request.Message, request.Serial)
		return contracts.DownlinkResult{Accepted: true, Serial: request.Serial}, nil
	}

	header := request.Message.GetHeader()
	if header == nil {
		return contracts.DownlinkResult{}, fmt.Errorf("downlink message header is required")
	}
	idempotencyKey := strings.TrimSpace(request.IdempotencyKey)
	if len(idempotencyKey) > 128 {
		return contracts.DownlinkResult{}, fmt.Errorf("Idempotency-Key must not exceed 128 characters")
	}
	commandID, err := newCommandID()
	if err != nil {
		return contracts.DownlinkResult{}, err
	}
	if idempotencyKey == "" {
		idempotencyKey = commandID
	}

	serial := request.Serial
	header.SerialNumber = serial
	encoded, err := request.Message.Encode()
	if err != nil {
		return contracts.DownlinkResult{}, fmt.Errorf("encode downlink message: %w", err)
	}
	headerBytes, err := header.Encode()
	if err != nil {
		return contracts.DownlinkResult{}, fmt.Errorf("encode downlink header: %w", err)
	}
	if len(headerBytes) >= len(encoded) {
		return contracts.DownlinkResult{}, fmt.Errorf("encoded downlink message has no body")
	}

	now := o.clock()
	command, created, err := o.commands.Enqueue(ctx, &repository.DownlinkCommand{
		CommandID:      commandID,
		IdempotencyKey: idempotencyKey,
		Phone:          header.PhoneNumber,
		MsgID:          header.MsgID,
		Serial:         serial,
		Payload:        encoded[len(headerBytes):],
		Status:         repository.CommandStatusPending,
		// initialWait 覆盖会话重传窗口，到期前不重投，避免与仍在重传的报文重复。
		NextAttemptAt: now.Add(o.opts.InitialWait),
		ExpiresAt:     now.Add(o.opts.ExpireAfter),
	})
	if err != nil {
		return contracts.DownlinkResult{}, fmt.Errorf("enqueue downlink command: %w", err)
	}
	result := resultFromCommand(command, !created)
	if !created {
		return result, nil
	}

	if err := o.send(request.SessionID, request.Message, serial); err != nil {
		// 首次下发失败不直接判死：保持 pending 并安排重投，设备重新上线后仍可补发。
		next := o.clock().Add(o.opts.RetryBase)
		_, _ = o.commands.ScheduleRetry(ctx, header.PhoneNumber, header.MsgID, serial, next, err.Error())
		return result, fmt.Errorf("send downlink command: %w", err)
	}
	return result, nil
}

// Query 实现 contracts.Downlink：按 commandId 查询终态。
func (o *Outbox) Query(ctx context.Context, id string) (contracts.DownlinkResult, error) {
	if !o.Enabled() {
		return contracts.DownlinkResult{}, repository.ErrCommandNotFound
	}
	command, err := o.commands.Get(ctx, id)
	if err != nil {
		return contracts.DownlinkResult{}, err
	}
	return resultFromCommand(command, false), nil
}

// Get 按 commandId 返回完整命令记录，供管理 API 展示。
func (o *Outbox) Get(ctx context.Context, id string) (*repository.DownlinkCommand, error) {
	if !o.Enabled() {
		return nil, repository.ErrCommandNotFound
	}
	return o.commands.Get(ctx, id)
}

// Stats 返回 pending 与已过期数量，供就绪检查与指标采集。
func (o *Outbox) Stats(ctx context.Context) (pending, expired int, err error) {
	if !o.Enabled() {
		return 0, 0, nil
	}
	return observability.OutboxStats(ctx, o.commands)
}

// CheckBacklog 在过期 pending 命令超过阈值时返回错误，用于就绪探针。
func (o *Outbox) CheckBacklog(ctx context.Context, threshold int) error {
	if !o.Enabled() {
		return nil
	}
	return observability.CheckOutboxBacklog(ctx, o.commands, threshold)
}

// Run 启动时立即重投一次，之后按 PollInterval 周期执行，直到 ctx 取消。
func (o *Outbox) Run(ctx context.Context) {
	defer close(o.done)
	if !o.Enabled() {
		return
	}
	if err := o.Tick(ctx); err != nil {
		log.Error().Err(err).Msg("Initial command replay tick failed")
	}
	ticker := time.NewTicker(o.opts.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := o.Tick(ctx); err != nil {
				log.Error().Err(err).Msg("Command replay tick failed")
			}
		}
	}
}

// Tick 执行一轮过期清理与重投，返回调度错误供调用方记录。
func (o *Outbox) Tick(ctx context.Context) error {
	if !o.Enabled() {
		return nil
	}
	now := o.clock()
	if expired, err := o.commands.ExpirePending(ctx, now, "expired before delivery"); err != nil {
		log.Error().Err(err).Msg("Failed to expire pending downlink commands")
	} else if expired > 0 {
		log.Info().Int("count", expired).Msg("Expired pending downlink commands")
	}

	claimed, err := o.commands.ClaimPending(ctx, repository.CommandClaim{
		Owner: o.owner,
		Now:   now,
		Limit: o.opts.BatchSize,
		Lease: o.opts.LeaseDuration,
	})
	if err != nil {
		return err
	}
	for _, command := range claimed {
		o.dispatch(ctx, command)
	}
	return nil
}

// dispatch 把一条已占用租约的命令投给在线会话，并对三种结果做明确落点：
// 会话不在线 → 释放租约等待；次数超限 → failed；已下发 → 等待应答事件。
func (o *Outbox) dispatch(ctx context.Context, command *repository.DownlinkCommand) {
	session, err := o.conn.GetByPhone(ctx, command.Phone)
	if err != nil || session == nil {
		o.release(ctx, command, "device offline")
		return
	}
	device, err := o.devices.GetByPhone(ctx, command.Phone)
	if err != nil {
		// 设备档案缺失时无法重建报文头（协议版本属性），保持等待并由过期兜底。
		o.release(ctx, command, "device profile unavailable")
		return
	}
	if command.Attempts >= o.opts.MaxAttempts {
		o.fail(ctx, command.Phone, command.MsgID, command.Serial, "retry attempts exhausted")
		return
	}

	now := o.clock()
	begun, err := o.commands.BeginAttempt(ctx, command.CommandID, o.owner, now)
	if err != nil {
		log.Error().Err(err).Str("command_id", command.CommandID).Msg("Failed to record command attempt")
		return
	}
	if !begun {
		// 租约已被回收（例如上一实例退出后过期），本轮不重复下发。
		log.Debug().Str("command_id", command.CommandID).Msg("Command lease was lost before dispatch")
		return
	}

	message := &model.MsgRawBody{
		Header: model.GenMsgHeader(device, command.MsgID, command.Serial),
		Body:   append([]byte(nil), command.Payload...),
	}
	if err := o.send(session.ID, message, command.Serial); err != nil {
		log.Error().Err(err).
			Str("command_id", command.CommandID).
			Str("phone", command.Phone).
			Uint16("msg_id", command.MsgID).
			Uint16("serial", command.Serial).
			Msg("Failed to replay downlink command")
		o.handleFailure(ctx, command.Phone, command.MsgID, command.Serial, err.Error())
		return
	}
	log.Info().
		Str("command_id", command.CommandID).
		Str("phone", command.Phone).
		Uint16("msg_id", command.MsgID).
		Uint16("serial", command.Serial).
		Int("attempt", command.Attempts+1).
		Msg("Replayed pending downlink command")
}

// HandleResult 消费会话的可靠投递结果事件：成功终结，失败按次数安排重投或转 failed。
func (o *Outbox) HandleResult(result model.ReliableResult) {
	if !o.Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	switch result.Status {
	case model.ReliableStatusAcked:
		updated, err := o.commands.MarkAcked(ctx, result.Phone, result.MsgID, result.Serial)
		if err != nil {
			log.Error().Err(err).
				Str("phone", result.Phone).
				Uint16("msg_id", result.MsgID).
				Uint16("serial", result.Serial).
				Msg("Failed to mark command acked")
			return
		}
		if !updated {
			log.Debug().
				Str("phone", result.Phone).
				Uint16("msg_id", result.MsgID).
				Uint16("serial", result.Serial).
				Msg("Acknowledgement did not match a pending command")
		}
	case model.ReliableStatusFailed:
		o.handleFailure(ctx, result.Phone, result.MsgID, result.Serial, result.Reason)
	}
}

// handleFailure 按当前 attempts 决定重投或终结；非 outbox 报文是无副作用的 no-op。
func (o *Outbox) handleFailure(ctx context.Context, phone string, msgID, serial uint16, reason string) {
	if reason == "" {
		reason = "delivery failed"
	}
	command, err := o.commands.FindByRouting(ctx, phone, msgID, serial)
	if err != nil {
		if err != repository.ErrCommandNotFound {
			log.Error().Err(err).
				Str("phone", phone).
				Uint16("msg_id", msgID).
				Uint16("serial", serial).
				Msg("Failed to look up command for retry")
		}
		return
	}
	if command.Status != repository.CommandStatusPending {
		return
	}
	if command.Attempts >= o.opts.MaxAttempts {
		o.fail(ctx, phone, msgID, serial, reason)
		return
	}
	next := o.clock().Add(o.retryDelay(command.Attempts))
	updated, err := o.commands.ScheduleRetry(ctx, phone, msgID, serial, next, reason)
	if err != nil {
		log.Error().Err(err).
			Str("phone", phone).
			Uint16("msg_id", msgID).
			Uint16("serial", serial).
			Msg("Failed to schedule command retry")
		return
	}
	log.Info().
		Str("phone", phone).
		Uint16("msg_id", msgID).
		Uint16("serial", serial).
		Int("attempts", command.Attempts).
		Time("next_attempt_at", next).
		Str("reason", reason).
		Msg("Scheduled downlink command retry")
	if !updated {
		log.Debug().
			Str("phone", phone).
			Uint16("msg_id", msgID).
			Uint16("serial", serial).
			Msg("Retry schedule did not match a pending command")
	}
}

// release 释放租约并保持 pending；设备离线不消耗重投次数。
func (o *Outbox) release(ctx context.Context, command *repository.DownlinkCommand, reason string) {
	updated, err := o.commands.ReleaseAttempt(ctx, command.CommandID, o.owner, o.clock(), reason)
	if err != nil {
		log.Error().Err(err).Str("command_id", command.CommandID).Msg("Failed to release command lease")
		return
	}
	log.Debug().
		Str("command_id", command.CommandID).
		Str("phone", command.Phone).
		Str("reason", reason).
		Bool("released", updated).
		Msg("Released command lease to wait")
}

// fail 把命令转 failed；失败原因写入 last_error 供运维排查。
func (o *Outbox) fail(ctx context.Context, phone string, msgID, serial uint16, reason string) {
	updated, err := o.commands.MarkFailed(ctx, phone, msgID, serial, reason)
	if err != nil {
		log.Error().Err(err).
			Str("phone", phone).
			Uint16("msg_id", msgID).
			Uint16("serial", serial).
			Msg("Failed to mark command as failed")
		return
	}
	if updated {
		log.Warn().
			Str("phone", phone).
			Uint16("msg_id", msgID).
			Uint16("serial", serial).
			Str("reason", reason).
			Msg("Downlink command marked failed")
	}
}

// retryDelay 按尝试次数指数退避，并受 RetryMax 上限约束。
func (o *Outbox) retryDelay(attempts int) time.Duration {
	delay := o.opts.RetryBase
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= o.opts.RetryMax {
			return o.opts.RetryMax
		}
	}
	if delay > o.opts.RetryMax {
		delay = o.opts.RetryMax
	}
	return delay
}

// resultFromCommand 把 outbox 记录转换成契约结果。
func resultFromCommand(command *repository.DownlinkCommand, duplicated bool) contracts.DownlinkResult {
	if command == nil {
		return contracts.DownlinkResult{Accepted: true}
	}
	return contracts.DownlinkResult{
		ID:         command.CommandID,
		Serial:     command.Serial,
		Status:     string(command.Status),
		Duplicated: duplicated,
		Accepted:   true,

		Phone:         command.Phone,
		MsgID:         command.MsgID,
		Attempts:      command.Attempts,
		LastError:     command.LastError,
		CreatedAt:     command.CreatedAt,
		UpdatedAt:     command.UpdatedAt,
		LastAttemptAt: command.LastAttemptAt,
		NextAttemptAt: command.NextAttemptAt,
		ExpiresAt:     command.ExpiresAt,
	}
}

func newCommandID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate command id: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

// newLeaseOwner 生成跨实例唯一的租约标识。
func newLeaseOwner() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	return fmt.Sprintf("%s-%d-%s", host, os.Getpid(), hex.EncodeToString(suffix))
}
