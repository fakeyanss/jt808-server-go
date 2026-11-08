// Package command 实现下行命令 outbox 的重投闭环。
//
// 职责边界：
//   - 只负责把 pending 命令重新投递给当前在线会话；
//   - 不构造业务报文，重投使用 outbox 中已保存的消息体与原始流水号；
//   - 不改变 outbox 终态，除非明确判定为过期或重投次数耗尽。
//
// 语义约定（与设计取舍相关，修改前请先确认）：
//  1. 复用原始流水号：outbox 与终端应答只通过 (Phone, MsgID, Serial) 关联，
//     重投若重新分配流水号，应答会落到新流水号上而无法回填原记录。代价是
//     at-least-once：终端可能收到并执行重复命令。
//  2. 设备离线保持等待：pending 命令不会因为设备不在线直接失败，而是释放
//     租约等待下一轮；最终由 expires_at 兜底转 failed。
//  3. 重投有上限：attempts 达到 maxAttempts 后转 failed，不做无限重试。
//  4. 并发保护：claim 时写入 lease_owner/lease_expires_at，同一命令在租约
//     有效期内只会被一个实例下发；租约到期后允许重新占用。
package command

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog/log"

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

// Options 是重投调度的运行时参数。
type Options struct {
	PollInterval  time.Duration
	BatchSize     int
	MaxAttempts   int
	LeaseDuration time.Duration
	RetryBase     time.Duration
	RetryMax      time.Duration
}

func (o Options) withDefaults() Options {
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

// Dependencies 是重投服务的外部依赖。
type Dependencies struct {
	Commands repository.CommandRepository
	Devices  repository.DeviceRepository
	Conn     runtime.ConnectionRegistry
	Send     Sender
	// Owner 是租约标识；为空时按主机名 + 进程号 + 随机串生成。
	Owner string
	// Clock 便于测试控制时间；为空时使用 time.Now。
	Clock func() time.Time
}

// Service 周期性地把 pending 命令重新投给在线会话。
type Service struct {
	commands repository.CommandRepository
	devices  repository.DeviceRepository
	conn     runtime.ConnectionRegistry
	send     Sender
	owner    string
	clock    func() time.Time
	opts     Options
	// done 在 Run 退出后关闭，供关机流程等待当前 tick 收尾。
	done chan struct{}
}

// NewService 校验依赖并创建重投服务。
func NewService(deps Dependencies, options Options) (*Service, error) {
	if deps.Commands == nil {
		return nil, fmt.Errorf("command replay: command repository is required")
	}
	if deps.Devices == nil {
		return nil, fmt.Errorf("command replay: device repository is required")
	}
	if deps.Conn == nil {
		return nil, fmt.Errorf("command replay: connection registry is required")
	}
	if deps.Send == nil {
		return nil, fmt.Errorf("command replay: sender is required")
	}
	owner := deps.Owner
	if owner == "" {
		owner = newLeaseOwner()
	}
	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Service{
		commands: deps.Commands,
		devices:  deps.Devices,
		conn:     deps.Conn,
		send:     deps.Send,
		owner:    owner,
		clock:    clock,
		opts:     options.withDefaults(),
		done:     make(chan struct{}),
	}, nil
}

// Done 在 Run 退出后关闭；未调用 Run 时永不关闭。
func (s *Service) Done() <-chan struct{} { return s.done }

// Run 启动时立即重投一次，之后按 PollInterval 周期执行，直到 ctx 取消。
func (s *Service) Run(ctx context.Context) {
	defer close(s.done)
	if err := s.Tick(ctx); err != nil {
		log.Error().Err(err).Msg("Initial command replay tick failed")
	}
	ticker := time.NewTicker(s.opts.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil {
				log.Error().Err(err).Msg("Command replay tick failed")
			}
		}
	}
}

// Tick 执行一轮过期清理与重投，返回调度错误供调用方记录。
func (s *Service) Tick(ctx context.Context) error {
	now := s.clock()
	if expired, err := s.commands.ExpirePending(ctx, now, "expired before delivery"); err != nil {
		log.Error().Err(err).Msg("Failed to expire pending downlink commands")
	} else if expired > 0 {
		log.Info().Int("count", expired).Msg("Expired pending downlink commands")
	}

	claimed, err := s.commands.ClaimPending(ctx, repository.CommandClaim{
		Owner: s.owner,
		Now:   now,
		Limit: s.opts.BatchSize,
		Lease: s.opts.LeaseDuration,
	})
	if err != nil {
		return err
	}
	for _, command := range claimed {
		s.dispatch(ctx, command)
	}
	return nil
}

// dispatch 把一条已占用租约的命令投给在线会话，并对三种结果做明确落点：
// 会话不在线 → 释放租约等待；次数超限 → failed；已下发 → 等待应答事件。
func (s *Service) dispatch(ctx context.Context, command *repository.DownlinkCommand) {
	session, err := s.conn.GetByPhone(ctx, command.Phone)
	if err != nil || session == nil {
		s.release(ctx, command, "device offline")
		return
	}
	device, err := s.devices.GetByPhone(ctx, command.Phone)
	if err != nil {
		// 设备档案缺失时无法重建报文头（协议版本属性），保持等待并由过期兜底。
		s.release(ctx, command, "device profile unavailable")
		return
	}
	if command.Attempts >= s.opts.MaxAttempts {
		s.fail(ctx, command.Phone, command.MsgID, command.Serial, "retry attempts exhausted")
		return
	}

	now := s.clock()
	begun, err := s.commands.BeginAttempt(ctx, command.CommandID, s.owner, now)
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
	if err := s.send(session.ID, message, command.Serial); err != nil {
		log.Error().Err(err).
			Str("command_id", command.CommandID).
			Str("phone", command.Phone).
			Uint16("msg_id", command.MsgID).
			Uint16("serial", command.Serial).
			Msg("Failed to replay downlink command")
		s.handleFailure(ctx, command.Phone, command.MsgID, command.Serial, err.Error())
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
func (s *Service) HandleResult(result model.ReliableResult) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	switch result.Status {
	case model.ReliableStatusAcked:
		updated, err := s.commands.MarkAcked(ctx, result.Phone, result.MsgID, result.Serial)
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
		s.handleFailure(ctx, result.Phone, result.MsgID, result.Serial, result.Reason)
	}
}

// handleFailure 按当前 attempts 决定重投或终结；非 outbox 报文是无副作用的 no-op。
func (s *Service) handleFailure(ctx context.Context, phone string, msgID, serial uint16, reason string) {
	if reason == "" {
		reason = "delivery failed"
	}
	command, err := s.commands.FindByRouting(ctx, phone, msgID, serial)
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
	if command.Attempts >= s.opts.MaxAttempts {
		s.fail(ctx, phone, msgID, serial, reason)
		return
	}
	next := s.clock().Add(s.retryDelay(command.Attempts))
	updated, err := s.commands.ScheduleRetry(ctx, phone, msgID, serial, next, reason)
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
func (s *Service) release(ctx context.Context, command *repository.DownlinkCommand, reason string) {
	updated, err := s.commands.ReleaseAttempt(ctx, command.CommandID, s.owner, s.clock(), reason)
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
func (s *Service) fail(ctx context.Context, phone string, msgID, serial uint16, reason string) {
	updated, err := s.commands.MarkFailed(ctx, phone, msgID, serial, reason)
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
func (s *Service) retryDelay(attempts int) time.Duration {
	delay := s.opts.RetryBase
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= s.opts.RetryMax {
			return s.opts.RetryMax
		}
	}
	if delay > s.opts.RetryMax {
		delay = s.opts.RetryMax
	}
	return delay
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
