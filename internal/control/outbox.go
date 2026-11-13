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
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/observability"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// 重投相关的默认值。可由配置覆盖。
const (
	DefaultPollInterval  = 10 * time.Second
	DefaultBatchSize     = 50
	DefaultMaxAttempts   = 5
	DefaultLeaseDuration = time.Minute
	DefaultRetryBase     = 5 * time.Second
	DefaultRetryMax      = 5 * time.Minute
	// DefaultOfflineRetry 是设备离线时的短退避，避免每个 poll tick 都
	// 重复 claim → offline → release 形成空转；不消耗尝试次数。
	DefaultOfflineRetry = 15 * time.Second
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
	// OfflineRetry 是设备离线时的重投间隔：租约释放、attempts 不变，
	// 但下一次尝试至少推迟这么久，避免每个 poll tick 空转。
	OfflineRetry time.Duration
	// RetryOnTerminalFailure 决定终端明确执行失败（Result=1）后是否重投。
	// 默认 false；Result=2/3 始终终结，不受该开关影响。
	RetryOnTerminalFailure bool
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
	if o.OfflineRetry <= 0 {
		o.OfflineRetry = DefaultOfflineRetry
	}
	return o
}

// OutboxDependencies 是内置管控的外部依赖。
type OutboxDependencies struct {
	Commands repository.CommandRepository
	Devices  repository.DeviceRepository
	// Send 是下行传输契约：本地直连与 HTTP 网关实现同一接口。
	// 离线判断一律看回执，不在控制面持有连接注册表（control-only 没有本地会话）。
	Send contracts.DownlinkSender
	// Metrics 收集终端应答处理结果；为空时只记日志。
	Metrics *observability.Metrics
	// Owner 是租约标识；为空时按主机名 + 进程号 + 随机串生成。
	Owner string
	// Clock 便于测试控制时间；为空时使用 time.Now。
	Clock func() time.Time
}

// Outbox 是内置管控的 durable 下行投递模块。
type Outbox struct {
	commands repository.CommandRepository
	devices  repository.DeviceRepository
	send     contracts.DownlinkSender
	metrics  *observability.Metrics
	owner    string
	clock    func() time.Time
	opts     OutboxOptions
	// done 在 Run 退出后关闭，供关机流程等待当前 tick 收尾。
	done chan struct{}
}

var _ contracts.TerminalReplyHandler = (*Outbox)(nil)

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
		send:     deps.Send,
		metrics:  deps.Metrics,
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
		if _, err := o.send.Send(ctx, request); err != nil {
			log.Warn().Err(err).Str("phone", request.Phone).Msg("Direct downlink send failed")
		}
		return contracts.DownlinkResult{Accepted: true, Serial: request.Serial}, nil
	}

	header := request.Message.GetHeader()
	if header == nil {
		return contracts.DownlinkResult{}, fmt.Errorf("downlink message header is required")
	}
	serial := request.Serial
	if serial == 0 {
		// 流水号由管控统一分配：off 模式由网关在发送时分配；落库模式必须先
		// 确定 (phone, msgId, serial) 才能关联终端应答并支持重投复用。
		allocated, err := o.commands.NextSerial(ctx, header.PhoneNumber, header.MsgID)
		if err != nil {
			return contracts.DownlinkResult{}, fmt.Errorf("allocate downlink serial: %w", err)
		}
		serial = allocated
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

	request.Serial = serial
	// 下行 HTTP 契约必须显式携带 phone：网关不解析编码后的消息头来路由。
	// 遗漏时首次下发会被网关以 400 invalid_request 拒绝，而重投路径又从命令记录里
	// 恢复了 phone，表现为“第一次必失败、重投才成功”的假象。
	request.Phone = header.PhoneNumber
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

	receipt, sendErr := o.send.Send(ctx, request)
	if sendErr != nil {
		classified := contracts.ClassifyDownlinkError(sendErr)
		if classified.IsPermanent() {
			// 400/401/403/413 是确定性拒绝：重投必然同样失败，直接终结并记录原因，
			// 不占用重投窗口与 in-flight 名额。
			o.fail(ctx, header.PhoneNumber, header.MsgID, serial, classified.Error())
			return result, fmt.Errorf("send downlink command: %w", sendErr)
		}
		// 可重试失败（429/5xx/网络/超时）保持 pending，按退避安排下一次。
		next := o.clock().Add(o.retryDelayFor(1, classified))
		_, _ = o.commands.ScheduleRetry(ctx, header.PhoneNumber, header.MsgID, serial, next, classified.Error())
		return result, fmt.Errorf("send downlink command: %w", sendErr)
	}
	switch receipt.Status {
	case contracts.DownlinkStatusOffline:
		// 设备不在线：保持 pending（不消耗尝试次数），等设备上线后由 Tick 重投。
		log.Info().
			Str("command_id", commandID).
			Str("phone", header.PhoneNumber).
			Msg("Downlink command is waiting for device to come online")
	case contracts.DownlinkStatusRejected:
		next := o.clock().Add(o.retryDelayFor(1, nil))
		reason := receipt.Reason
		if reason == "" {
			reason = "gateway rejected downlink"
		}
		_, _ = o.commands.ScheduleRetry(ctx, header.PhoneNumber, header.MsgID, serial, next, reason)
		return result, fmt.Errorf("send downlink command: %s", reason)
	case contracts.DownlinkStatusAcked, contracts.DownlinkStatusFailed:
		o.handleReceipt(ctx, contracts.DownlinkRequest{
			Phone:   header.PhoneNumber,
			Serial:  serial,
			Message: request.Message,
		}, receipt)
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

// dispatch 把一条已占用租约的命令交给下行契约，并对回执做明确落点：
// offline → 释放租约且不消耗尝试次数；rejected/可重试错误 → 退避重投；
// 永久错误 → 立即终结；acked/failed → 推进状态。
func (o *Outbox) dispatch(ctx context.Context, command *repository.DownlinkCommand) {
	request := contracts.DownlinkRequest{
		Phone:          command.Phone,
		SessionID:      "",
		IdempotencyKey: command.CommandID,
		Serial:         command.Serial,
	}
	device, err := o.devices.GetByPhone(ctx, command.Phone)
	if err != nil {
		// 设备档案缺失时无法重建报文头（协议版本属性），保持等待并由过期兜底。
		o.releaseOffline(ctx, command, "device profile unavailable")
		return
	}
	if command.Attempts >= o.opts.MaxAttempts {
		o.fail(ctx, command.Phone, command.MsgID, command.Serial, "retry attempts exhausted")
		return
	}

	// 记录占用前的尝试快照，供离线释放时原样恢复。
	previousAttempts := command.Attempts
	previousLastAttempt := command.LastAttemptAt

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
	attempts := previousAttempts + 1

	request.Message = &model.MsgRawBody{
		Header: model.GenMsgHeader(device, command.MsgID, command.Serial),
		Body:   append([]byte(nil), command.Payload...),
	}
	receipt, err := o.send.Send(ctx, request)
	if err != nil {
		classified := contracts.ClassifyDownlinkError(err)
		log.Error().Err(err).
			Str("command_id", command.CommandID).
			Str("phone", command.Phone).
			Uint16("msg_id", command.MsgID).
			Uint16("serial", command.Serial).
			Bool("permanent", classified.IsPermanent()).
			Msg("Failed to replay downlink command")
		if classified.IsPermanent() {
			o.fail(ctx, command.Phone, command.MsgID, command.Serial, classified.Error())
			return
		}
		o.releaseAfterFailure(ctx, command, attempts, classified)
		return
	}
	switch receipt.Status {
	case contracts.DownlinkStatusOffline:
		o.releaseOfflineWithSnapshot(ctx, command, previousAttempts, previousLastAttempt, "device offline")
		return
	case contracts.DownlinkStatusRejected:
		reason := receipt.Reason
		if reason == "" {
			reason = "gateway rejected downlink"
		}
		o.releaseAfterFailure(ctx, command, attempts, &contracts.DownlinkError{
			Kind:    contracts.DownlinkErrorTransient,
			Message: reason,
		})
		return
	case contracts.DownlinkStatusAcked, contracts.DownlinkStatusFailed:
		o.handleReceipt(ctx, request, receipt)
		return
	}
	log.Info().
		Str("command_id", command.CommandID).
		Str("phone", command.Phone).
		Uint16("msg_id", command.MsgID).
		Uint16("serial", command.Serial).
		Int("attempt", attempts).
		Msg("Replayed pending downlink command")
}

// handleReceipt 把网关同步回执转成终端应答，复用统一状态机。
func (o *Outbox) handleReceipt(ctx context.Context, request contracts.DownlinkRequest, receipt contracts.DownlinkReceipt) {
	if receipt.Result == nil {
		return
	}
	header := request.Message.GetHeader()
	if header == nil {
		return
	}
	reply := contracts.TerminalReply{
		EventID:      fmt.Sprintf("%s|0x%04X|%d", request.Phone, header.MsgID, request.Serial),
		Phone:        request.Phone,
		AnswerMsgID:  header.MsgID,
		AnswerSerial: request.Serial,
		Result:       *receipt.Result,
		Source:       "http_receipt",
	}
	if _, err := o.HandleTerminalReply(ctx, reply); err != nil {
		log.Error().Err(err).
			Str("phone", request.Phone).
			Uint16("msg_id", header.MsgID).
			Uint16("serial", request.Serial).
			Msg("Failed to handle gateway receipt as terminal reply")
	}
}

// HandleResult 消费会话的可靠投递结果事件。
//
// 这是 local 出口的适配器：成功应答（含 0x0001 的失败结果）转交统一入口
// HandleTerminalReply；写失败/重传耗尽仍走传输层重投逻辑。
func (o *Outbox) HandleResult(result model.ReliableResult) {
	if !o.Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	switch result.Status {
	case model.ReliableStatusAcked:
		terminalResult := uint8(0)
		if result.TerminalResult != nil {
			terminalResult = *result.TerminalResult
		}
		reply := contracts.TerminalReply{
			EventID:      fmt.Sprintf("%s|0x%04X|%d", result.Phone, result.MsgID, result.Serial),
			Phone:        result.Phone,
			AnswerMsgID:  result.MsgID,
			AnswerSerial: result.Serial,
			Result:       terminalResult,
			Source:       "transport",
		}
		if _, err := o.HandleTerminalReply(ctx, reply); err != nil {
			log.Error().Err(err).
				Str("phone", result.Phone).
				Uint16("msg_id", result.MsgID).
				Uint16("serial", result.Serial).
				Msg("Failed to handle terminal reply from transport")
		}
	case model.ReliableStatusFailed:
		o.handleFailure(ctx, result.Phone, result.MsgID, result.Serial, result.Reason)
	}
}

// HandleTerminalReply 是终端应答的统一入口。
//
// local 会话回调与 http 事件消费方都调用这里；状态推进、幂等、迟到与
// 无匹配分类只有一份实现。匹配键为 (phone, answerMsgID, answerSerial)。
func (o *Outbox) HandleTerminalReply(ctx context.Context, reply contracts.TerminalReply) (contracts.TerminalReplyOutcome, error) {
	if !o.Enabled() {
		return contracts.TerminalReplyIgnored, nil
	}
	if reply.Phone == "" || reply.AnswerMsgID == 0 {
		o.observeTerminalReply(contracts.TerminalReplyUnmatched)
		log.Warn().
			Str("event_id", reply.EventID).
			Str("source", reply.Source).
			Str("phone", reply.Phone).
			Uint16("answer_msg_id", reply.AnswerMsgID).
			Uint16("answer_serial", reply.AnswerSerial).
			Msg("Terminal reply is missing phone or answer message id")
		return contracts.TerminalReplyUnmatched, nil
	}

	command, err := o.commands.FindByRouting(ctx, reply.Phone, reply.AnswerMsgID, reply.AnswerSerial)
	if err != nil {
		if errors.Is(err, repository.ErrCommandNotFound) {
			o.observeTerminalReply(contracts.TerminalReplyUnmatched)
			log.Warn().
				Str("event_id", reply.EventID).
				Str("source", reply.Source).
				Str("phone", reply.Phone).
				Uint16("answer_msg_id", reply.AnswerMsgID).
				Uint16("answer_serial", reply.AnswerSerial).
				Uint8("result", reply.Result).
				Msg("Terminal reply has no matching outbox command")
			return contracts.TerminalReplyUnmatched, nil
		}
		return "", fmt.Errorf("look up terminal reply command: %w", err)
	}
	if command.Status != repository.CommandStatusPending {
		outcome := classifyTerminalReply(command.Status, reply.Result)
		o.observeTerminalReply(outcome)
		log.Info().
			Str("event_id", reply.EventID).
			Str("source", reply.Source).
			Str("command_id", command.CommandID).
			Str("phone", reply.Phone).
			Uint16("answer_msg_id", reply.AnswerMsgID).
			Uint16("answer_serial", reply.AnswerSerial).
			Uint8("result", reply.Result).
			Str("status", string(command.Status)).
			Str("outcome", string(outcome)).
			Msg("Ignored terminal reply for terminal command")
		return outcome, nil
	}

	// 命令已过 ExpiresAt 但 Tick 尚未清理：按迟到处理，先终结为 failed，绝不改回 acked。
	if !command.ExpiresAt.IsZero() && !command.ExpiresAt.After(o.clock()) {
		if _, err := o.commands.MarkFailed(ctx, reply.Phone, reply.AnswerMsgID, reply.AnswerSerial, "expired before reply"); err != nil {
			return "", fmt.Errorf("mark expired command failed: %w", err)
		}
		o.observeTerminalReply(contracts.TerminalReplyLate)
		log.Info().
			Str("command_id", command.CommandID).
			Str("phone", reply.Phone).
			Uint16("msg_id", reply.AnswerMsgID).
			Uint16("serial", reply.AnswerSerial).
			Msg("Ignored late terminal reply for expired command")
		return contracts.TerminalReplyLate, nil
	}

	if reply.Result == 0 {
		updated, err := o.commands.MarkAcked(ctx, reply.Phone, reply.AnswerMsgID, reply.AnswerSerial)
		if err != nil {
			return "", fmt.Errorf("mark command acked: %w", err)
		}
		if !updated {
			return o.classifyTerminalReplyRace(ctx, reply)
		}
		o.observeTerminalReply(contracts.TerminalReplyAcked)
		log.Info().
			Str("command_id", command.CommandID).
			Str("phone", reply.Phone).
			Uint16("msg_id", reply.AnswerMsgID).
			Uint16("serial", reply.AnswerSerial).
			Msg("Terminal reply acknowledged downlink command")
		return contracts.TerminalReplyAcked, nil
	}

	// 终端明确执行失败：默认终结，不重投；仅 Result=1 且显式开启开关时重投。
	if o.opts.RetryOnTerminalFailure && reply.Result == 1 && command.Attempts < o.opts.MaxAttempts {
		next := o.clock().Add(o.retryDelay(command.Attempts))
		updated, err := o.commands.ScheduleRetry(ctx, reply.Phone, reply.AnswerMsgID, reply.AnswerSerial, next, terminalFailureReason(reply.Result))
		if err != nil {
			return "", fmt.Errorf("schedule retry after terminal failure: %w", err)
		}
		if !updated {
			return o.classifyTerminalReplyRace(ctx, reply)
		}
		o.observeTerminalReply(contracts.TerminalReplyRetryScheduled)
		log.Info().
			Str("command_id", command.CommandID).
			Str("phone", reply.Phone).
			Uint16("msg_id", reply.AnswerMsgID).
			Uint16("serial", reply.AnswerSerial).
			Uint8("result", reply.Result).
			Int("attempts", command.Attempts).
			Time("next_attempt_at", next).
			Msg("Scheduled retry after terminal failure reply")
		return contracts.TerminalReplyRetryScheduled, nil
	}

	updated, err := o.commands.MarkFailed(ctx, reply.Phone, reply.AnswerMsgID, reply.AnswerSerial, terminalFailureReason(reply.Result))
	if err != nil {
		return "", fmt.Errorf("mark command failed after terminal reply: %w", err)
	}
	if !updated {
		return o.classifyTerminalReplyRace(ctx, reply)
	}
	o.observeTerminalReply(contracts.TerminalReplyFailed)
	log.Warn().
		Str("command_id", command.CommandID).
		Str("phone", reply.Phone).
		Uint16("msg_id", reply.AnswerMsgID).
		Uint16("serial", reply.AnswerSerial).
		Uint8("result", reply.Result).
		Msg("Terminal failure reply marked command failed")
	return contracts.TerminalReplyFailed, nil
}

// classifyTerminalReplyRace 在并发推进后重新读取命令，明确属于重复还是迟到。
func (o *Outbox) classifyTerminalReplyRace(ctx context.Context, reply contracts.TerminalReply) (contracts.TerminalReplyOutcome, error) {
	command, err := o.commands.FindByRouting(ctx, reply.Phone, reply.AnswerMsgID, reply.AnswerSerial)
	if err != nil {
		if errors.Is(err, repository.ErrCommandNotFound) {
			o.observeTerminalReply(contracts.TerminalReplyUnmatched)
			return contracts.TerminalReplyUnmatched, nil
		}
		return "", fmt.Errorf("re-read command after reply race: %w", err)
	}
	outcome := classifyTerminalReply(command.Status, reply.Result)
	o.observeTerminalReply(outcome)
	return outcome, nil
}

// classifyTerminalReply 把终态命令与应答结果对比：
// 结果与终态一致视为重复应答；结果矛盾或迟到视为 late。
func classifyTerminalReply(status repository.CommandStatus, result uint8) contracts.TerminalReplyOutcome {
	switch {
	case status == repository.CommandStatusAcked && result == 0:
		return contracts.TerminalReplyDuplicate
	case status == repository.CommandStatusFailed && result != 0:
		return contracts.TerminalReplyDuplicate
	default:
		return contracts.TerminalReplyLate
	}
}

func (o *Outbox) observeTerminalReply(outcome contracts.TerminalReplyOutcome) {
	if o.metrics != nil {
		o.metrics.IncTerminalReply(string(outcome))
	}
}

func terminalFailureReason(result uint8) string {
	switch result {
	case 1:
		return "terminal execution failed"
	case 2:
		return "terminal reported malformed message"
	case 3:
		return "terminal reported unsupported command"
	default:
		return fmt.Sprintf("terminal reply result %d", result)
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

// releaseOffline 释放租约但保持 pending，用于设备离线或档案未就绪等“未真正下发”的场景。
//
// 与 ReleaseAttempt 的区别：ReleaseAttempt 用于真实下发失败后的退避，会写入
// 下一次可投时刻且 attempts 已由 BeginAttempt 递增；ReleaseOffline 则把
// attempts/last_attempt_at 回退到占用前快照，避免设备长期离线期间把
// MaxAttempts 这道闸反复刷掉。两者都会写入下一次可投时刻：离线用较短的
// OfflineRetry，避免每个 poll tick 都重复占用租约空转。
func (o *Outbox) releaseOffline(ctx context.Context, command *repository.DownlinkCommand, reason string) {
	o.releaseOfflineWithSnapshot(ctx, command, command.Attempts, command.LastAttemptAt, reason)
}

func (o *Outbox) releaseOfflineWithSnapshot(ctx context.Context, command *repository.DownlinkCommand, attempts int, lastAttemptAt time.Time, reason string) {
	next := o.clock().Add(o.opts.OfflineRetry)
	updated, err := o.commands.ReleaseOffline(ctx, command.CommandID, o.owner, attempts, lastAttemptAt, next, reason)
	if err != nil {
		log.Error().Err(err).Str("command_id", command.CommandID).Msg("Failed to release command lease while offline")
		return
	}
	log.Debug().
		Str("command_id", command.CommandID).
		Str("phone", command.Phone).
		Str("reason", reason).
		Bool("released", updated).
		Msg("Released command lease to wait for device")
}

// releaseAfterFailure 处理真实下发失败：未到上限则按退避重投，否则终结。
func (o *Outbox) releaseAfterFailure(ctx context.Context, command *repository.DownlinkCommand, attempts int, failure *contracts.DownlinkError) {
	reason := failure.Error()
	if attempts >= o.opts.MaxAttempts {
		o.fail(ctx, command.Phone, command.MsgID, command.Serial, reason)
		return
	}
	next := o.clock().Add(o.retryDelayFor(attempts, failure))
	updated, err := o.commands.ReleaseAttempt(ctx, command.CommandID, o.owner, next, reason)
	if err != nil {
		log.Error().Err(err).Str("command_id", command.CommandID).Msg("Failed to release command lease after failure")
		return
	}
	log.Info().
		Str("command_id", command.CommandID).
		Str("phone", command.Phone).
		Uint16("msg_id", command.MsgID).
		Uint16("serial", command.Serial).
		Int("attempts", attempts).
		Time("next_attempt_at", next).
		Str("reason", reason).
		Bool("released", updated).
		Msg("Scheduled downlink command retry after send failure")
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

// retryDelayFor 计算下一次重投间隔：优先尊重网关的 Retry-After（429 限流），
// 否则按尝试次数指数退避。RetryMax 同时约束两种情况，避免无限挂起。
func (o *Outbox) retryDelayFor(attempts int, failure *contracts.DownlinkError) time.Duration {
	if failure != nil && failure.RetryAfter > 0 {
		if failure.RetryAfter > o.opts.RetryMax {
			return o.opts.RetryMax
		}
		return failure.RetryAfter
	}
	return o.retryDelay(attempts)
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
