package control

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

func replyFor(command *repository.DownlinkCommand, result uint8) contracts.TerminalReply {
	return contracts.TerminalReply{
		EventID:      "event-" + command.CommandID,
		Phone:        command.Phone,
		AnswerMsgID:  command.MsgID,
		AnswerSerial: command.Serial,
		Result:       result,
		Source:       "http",
	}
}

func uint8Ptr(v uint8) *uint8 { return &v }

// TestHandleTerminalReplyAckedAndDuplicate 覆盖成功应答与重复应答：
// 首次推进为 acked 并清租约，重复到达只计 duplicate，不重复推进。
func TestHandleTerminalReplyAckedAndDuplicate(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, defaultTestOptions())
	command := h.enqueue(t, "cmd-reply-ack", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

	outcome, err := h.service.HandleTerminalReply(ctx, replyFor(command, 0))
	require.NoError(t, err)
	require.Equal(t, contracts.TerminalReplyAcked, outcome)

	stored, err := h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusAcked, stored.Status)
	require.Empty(t, stored.LeaseOwner)

	snapshot := h.metrics.TerminalReplySnapshot()
	require.Equal(t, int64(1), snapshot.Acked)
	require.Equal(t, int64(0), snapshot.Duplicate)
	require.Equal(t, int64(0), snapshot.Late)

	// 同一条应答再次到达：状态不变、计数不重复。
	outcome, err = h.service.HandleTerminalReply(ctx, replyFor(command, 0))
	require.NoError(t, err)
	require.Equal(t, contracts.TerminalReplyDuplicate, outcome)

	snapshot = h.metrics.TerminalReplySnapshot()
	require.Equal(t, int64(1), snapshot.Acked)
	require.Equal(t, int64(1), snapshot.Duplicate)
	require.Equal(t, int64(0), snapshot.Late)

	stored, err = h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusAcked, stored.Status)
}

// TestHandleTerminalReplyFailureTerminalByDefault 覆盖终端失败应答：
// 默认终结为 failed，不安排重投，且不把失败当成成功。
func TestHandleTerminalReplyFailureTerminalByDefault(t *testing.T) {
	ctx := context.Background()
	for _, result := range []uint8{1, 2, 3} {
		result := result
		t.Run(fmt.Sprintf("result=%d", result), func(t *testing.T) {
			h := newHarness(t, defaultTestOptions())
			command := h.enqueue(t, "cmd-reply-failed", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

			outcome, err := h.service.HandleTerminalReply(ctx, replyFor(command, result))
			require.NoError(t, err)
			require.Equal(t, contracts.TerminalReplyFailed, outcome)

			stored, err := h.repo.Get(ctx, command.CommandID)
			require.NoError(t, err)
			require.Equal(t, repository.CommandStatusFailed, stored.Status)
			require.Equal(t, terminalFailureReason(result), stored.LastError)
			require.Empty(t, stored.LeaseOwner)

			snapshot := h.metrics.TerminalReplySnapshot()
			require.Equal(t, int64(1), snapshot.Failed)
			require.Equal(t, int64(0), snapshot.RetryScheduled)
			require.Equal(t, int64(0), snapshot.Acked)
			require.Equal(t, 0, h.sender.count(), "terminal failure must not trigger an immediate resend")
		})
	}
}

// TestHandleTerminalReplyFailureRetryWhenConfigured 覆盖可选重投：
// 仅 Result=1 且在次数上限内才安排重投；Result=2/3 始终终结。
func TestHandleTerminalReplyFailureRetryWhenConfigured(t *testing.T) {
	ctx := context.Background()
	options := defaultTestOptions()
	options.RetryOnTerminalFailure = true
	options.MaxAttempts = 3

	h := newHarness(t, options)
	command := h.enqueue(t, "cmd-reply-retry", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

	outcome, err := h.service.HandleTerminalReply(ctx, replyFor(command, 1))
	require.NoError(t, err)
	require.Equal(t, contracts.TerminalReplyRetryScheduled, outcome)

	stored, err := h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusPending, stored.Status)
	require.Empty(t, stored.LeaseOwner)
	require.Equal(t, h.clock.now.Add(options.RetryBase), stored.NextAttemptAt)
	require.Equal(t, int64(1), h.metrics.TerminalReplySnapshot().RetryScheduled)

	// Result=2/3 是确定性错误，即使开启开关也不重投。
	for _, result := range []uint8{2, 3} {
		result := result
		t.Run(fmt.Sprintf("result=%d", result), func(t *testing.T) {
			h := newHarness(t, options)
			command := h.enqueue(t, "cmd-reply-permanent", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

			outcome, err := h.service.HandleTerminalReply(ctx, replyFor(command, result))
			require.NoError(t, err)
			require.Equal(t, contracts.TerminalReplyFailed, outcome)

			stored, err := h.repo.Get(ctx, command.CommandID)
			require.NoError(t, err)
			require.Equal(t, repository.CommandStatusFailed, stored.Status)
			require.Equal(t, int64(0), h.metrics.TerminalReplySnapshot().RetryScheduled)
		})
	}
}

// TestHandleTerminalReplyFailureExhausted 覆盖重投开关开启但次数已到上限：
// 直接终结，不再安排重投。
func TestHandleTerminalReplyFailureExhausted(t *testing.T) {
	ctx := context.Background()
	options := defaultTestOptions()
	options.RetryOnTerminalFailure = true
	options.MaxAttempts = 3

	h := newHarness(t, options)
	command := h.enqueue(t, "cmd-reply-exhausted", options.MaxAttempts, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

	outcome, err := h.service.HandleTerminalReply(ctx, replyFor(command, 1))
	require.NoError(t, err)
	require.Equal(t, contracts.TerminalReplyFailed, outcome)

	stored, err := h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusFailed, stored.Status)
}

// TestHandleTerminalReplyLateDoesNotRevive 覆盖迟到应答：
// 已终结命令不会被成功应答改回 acked；已过期但尚未清理的命令按迟到终结。
func TestHandleTerminalReplyLateDoesNotRevive(t *testing.T) {
	ctx := context.Background()

	t.Run("failed command", func(t *testing.T) {
		h := newHarness(t, defaultTestOptions())
		command := h.enqueue(t, "cmd-late-failed", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

		outcome, err := h.service.HandleTerminalReply(ctx, replyFor(command, 1))
		require.NoError(t, err)
		require.Equal(t, contracts.TerminalReplyFailed, outcome)

		outcome, err = h.service.HandleTerminalReply(ctx, replyFor(command, 0))
		require.NoError(t, err)
		require.Equal(t, contracts.TerminalReplyLate, outcome)

		stored, err := h.repo.Get(ctx, command.CommandID)
		require.NoError(t, err)
		require.Equal(t, repository.CommandStatusFailed, stored.Status)

		snapshot := h.metrics.TerminalReplySnapshot()
		require.Equal(t, int64(0), snapshot.Acked)
		require.Equal(t, int64(1), snapshot.Failed)
		require.Equal(t, int64(1), snapshot.Late)
	})

	t.Run("expired but not yet swept", func(t *testing.T) {
		h := newHarness(t, defaultTestOptions())
		command := h.enqueue(t, "cmd-late-expired", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(-time.Second))

		outcome, err := h.service.HandleTerminalReply(ctx, replyFor(command, 0))
		require.NoError(t, err)
		require.Equal(t, contracts.TerminalReplyLate, outcome)

		stored, err := h.repo.Get(ctx, command.CommandID)
		require.NoError(t, err)
		require.Equal(t, repository.CommandStatusFailed, stored.Status)
		require.Equal(t, "expired before reply", stored.LastError)
		require.Equal(t, int64(1), h.metrics.TerminalReplySnapshot().Late)
		require.Equal(t, int64(0), h.metrics.TerminalReplySnapshot().Acked)
	})
}

// TestHandleTerminalReplyUnmatched 覆盖无匹配应答：没有对应 outbox 记录时
// 不静默丢弃，计入 unmatched；缺少必要字段也归入 unmatched。
func TestHandleTerminalReplyUnmatched(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, defaultTestOptions())

	outcome, err := h.service.HandleTerminalReply(ctx, contracts.TerminalReply{
		EventID:      "event-unknown",
		Phone:        testPhone,
		AnswerMsgID:  0x8105,
		AnswerSerial: 1234,
		Result:       0,
		Source:       "http",
	})
	require.NoError(t, err)
	require.Equal(t, contracts.TerminalReplyUnmatched, outcome)
	require.Equal(t, int64(1), h.metrics.TerminalReplySnapshot().Unmatched)

	outcome, err = h.service.HandleTerminalReply(ctx, contracts.TerminalReply{
		EventID: "event-invalid",
		Phone:   testPhone,
		Result:  0,
	})
	require.NoError(t, err)
	require.Equal(t, contracts.TerminalReplyUnmatched, outcome)
	require.Equal(t, int64(2), h.metrics.TerminalReplySnapshot().Unmatched)
}

// TestTerminalReplyTransportAndHTTPDoNotDoubleCount 覆盖同进程双来源：
// 传输层 ack 与 HTTP 事件消费方先后处理同一条命令，第二次只计 duplicate，
// 不污染 late，也不重复推进状态。
func TestTerminalReplyTransportAndHTTPDoNotDoubleCount(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		h := newHarness(t, defaultTestOptions())
		command := h.enqueue(t, "cmd-dual-ok", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

		h.service.HandleResult(model.ReliableResult{
			MsgID:          command.MsgID,
			Serial:         command.Serial,
			Phone:          command.Phone,
			Status:         model.ReliableStatusAcked,
			TerminalResult: uint8Ptr(0),
		})
		outcome, err := h.service.HandleTerminalReply(ctx, replyFor(command, 0))
		require.NoError(t, err)
		require.Equal(t, contracts.TerminalReplyDuplicate, outcome)

		stored, err := h.repo.Get(ctx, command.CommandID)
		require.NoError(t, err)
		require.Equal(t, repository.CommandStatusAcked, stored.Status)

		snapshot := h.metrics.TerminalReplySnapshot()
		require.Equal(t, int64(1), snapshot.Acked)
		require.Equal(t, int64(1), snapshot.Duplicate)
		require.Equal(t, int64(0), snapshot.Late)
	})

	t.Run("failure", func(t *testing.T) {
		h := newHarness(t, defaultTestOptions())
		command := h.enqueue(t, "cmd-dual-fail", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

		h.service.HandleResult(model.ReliableResult{
			MsgID:          command.MsgID,
			Serial:         command.Serial,
			Phone:          command.Phone,
			Status:         model.ReliableStatusAcked,
			TerminalResult: uint8Ptr(1),
		})
		outcome, err := h.service.HandleTerminalReply(ctx, replyFor(command, 1))
		require.NoError(t, err)
		require.Equal(t, contracts.TerminalReplyDuplicate, outcome)

		stored, err := h.repo.Get(ctx, command.CommandID)
		require.NoError(t, err)
		require.Equal(t, repository.CommandStatusFailed, stored.Status)

		snapshot := h.metrics.TerminalReplySnapshot()
		require.Equal(t, int64(1), snapshot.Failed)
		require.Equal(t, int64(1), snapshot.Duplicate)
		require.Equal(t, int64(0), snapshot.Late)
	})
}

// TestTerminalReplyAckStopsReplay 覆盖租约收尾：应答到达后租约清空，
// 后续 Tick 不再重投。
func TestTerminalReplyAckStopsReplay(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, defaultTestOptions())
	h.bindOnline(t)
	command := h.enqueue(t, "cmd-reply-lease", 1, h.clock.now.Add(-time.Minute), h.clock.now.Add(time.Hour))

	require.NoError(t, h.service.Tick(ctx))
	require.Equal(t, 1, h.sender.count())

	stored, err := h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.NotEmpty(t, stored.LeaseOwner)

	outcome, err := h.service.HandleTerminalReply(ctx, replyFor(command, 0))
	require.NoError(t, err)
	require.Equal(t, contracts.TerminalReplyAcked, outcome)

	stored, err = h.repo.Get(ctx, command.CommandID)
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusAcked, stored.Status)
	require.Empty(t, stored.LeaseOwner)

	require.NoError(t, h.service.Tick(ctx))
	require.Equal(t, 1, h.sender.count(), "acknowledged command must not be replayed")
}

// TestTerminalReplyIgnoredWhenOutboxDisabled 覆盖 off 模式：应答入口不改变任何状态。
func TestTerminalReplyIgnoredWhenOutboxDisabled(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, OutboxOptions{Enabled: false, PollInterval: time.Second, BatchSize: 10, MaxAttempts: 5})

	outcome, err := h.service.HandleTerminalReply(ctx, contracts.TerminalReply{
		Phone:        testPhone,
		AnswerMsgID:  0x8105,
		AnswerSerial: 77,
		Result:       0,
	})
	require.NoError(t, err)
	require.Equal(t, contracts.TerminalReplyIgnored, outcome)
	require.Equal(t, int64(0), h.metrics.TerminalReplySnapshot().Unmatched)
}
