package memory

import (
	"context"
	"testing"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestCommandRepositoryMemoryNextSerialSkipsInFlight(t *testing.T) {
	ctx := context.Background()
	const phone = "188765471001"

	// 回绕场景：游标从该 phone 已存在的最大流水号之后开始，65535 之后回到 1。
	repo := NewCommandRepository()
	_, _, err := repo.Enqueue(ctx, &repository.DownlinkCommand{
		CommandID: "seed", IdempotencyKey: "seed", Phone: phone, MsgID: 0x8201, Serial: 65535,
	}, repository.CommandSchedule{})
	require.NoError(t, err)
	_, _, err = repo.Enqueue(ctx, &repository.DownlinkCommand{
		CommandID: "in-flight", IdempotencyKey: "in-flight", Phone: phone, MsgID: 0x8105, Serial: 1,
	}, repository.CommandSchedule{})
	require.NoError(t, err)

	serial, err := repo.NextSerial(ctx, phone, 0x8105)
	require.NoError(t, err)
	require.Equal(t, uint16(2), serial, "回绕后必须跳过同 msgId 仍在途的 serial")
}

func TestCommandRepositoryMemoryIdempotencyAndResult(t *testing.T) {
	ctx := context.Background()
	repo := NewCommandRepository()

	first, created, err := repo.Enqueue(ctx, &repository.DownlinkCommand{
		CommandID:      "cmd-1",
		IdempotencyKey: "idem-1",
		Phone:          "188765470001",
		MsgID:          0x8105,
		Serial:         7,
		Payload:        []byte{0x02},
	}, repository.CommandSchedule{})
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, repository.CommandStatusPending, first.Status)
	require.Equal(t, 1, first.Attempts)

	// 同设备同幂等键返回已有记录，不覆盖原 commandId。
	duplicate, created, err := repo.Enqueue(ctx, &repository.DownlinkCommand{
		CommandID:      "cmd-2",
		IdempotencyKey: "idem-1",
		Phone:          "188765470001",
		MsgID:          0x8105,
		Serial:         8,
	}, repository.CommandSchedule{})
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, "cmd-1", duplicate.CommandID)
	require.Equal(t, uint16(7), duplicate.Serial)

	// 不同设备复用同一幂等键互不影响。
	otherPhone, created, err := repo.Enqueue(ctx, &repository.DownlinkCommand{
		CommandID:      "cmd-3",
		IdempotencyKey: "idem-1",
		Phone:          "188765470002",
		MsgID:          0x8105,
		Serial:         9,
	}, repository.CommandSchedule{})
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "cmd-3", otherPhone.CommandID)

	updated, err := repo.MarkAcked(ctx, "188765470001", 0x8105, 7)
	require.NoError(t, err)
	require.True(t, updated)
	// 已进入终态后重复回填返回 false。
	updated, err = repo.MarkAcked(ctx, "188765470001", 0x8105, 7)
	require.NoError(t, err)
	require.False(t, updated)

	stored, err := repo.Get(ctx, "cmd-1")
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusAcked, stored.Status)
	require.Empty(t, stored.LastError)

	failed, err := repo.MarkFailed(ctx, "188765470002", 0x8105, 9, "retransmission exhausted")
	require.NoError(t, err)
	require.True(t, failed)
	stored, err = repo.Get(ctx, "cmd-3")
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusFailed, stored.Status)
	require.Equal(t, "retransmission exhausted", stored.LastError)

	_, err = repo.Get(ctx, "missing")
	require.ErrorIs(t, err, repository.ErrCommandNotFound)
}

func TestCommandRepositoryMemoryKeepsRoutingForTerminalCommands(t *testing.T) {
	ctx := context.Background()
	repo := NewCommandRepository()
	const phone = "188765470003"

	_, created, err := repo.Enqueue(ctx, &repository.DownlinkCommand{
		CommandID:      "cmd-terminal",
		IdempotencyKey: "idem-terminal",
		Phone:          phone,
		MsgID:          0x8105,
		Serial:         31,
		Payload:        []byte{0x02},
	}, repository.CommandSchedule{ExpireAfter: time.Hour})
	require.NoError(t, err)
	require.True(t, created)

	updated, err := repo.MarkAcked(ctx, phone, 0x8105, 31)
	require.NoError(t, err)
	require.True(t, updated)

	// 终态记录仍需可被 routing 命中，供迟到/重复应答分类。
	stored, err := repo.FindByRouting(ctx, phone, 0x8105, 31)
	require.NoError(t, err)
	require.Equal(t, "cmd-terminal", stored.CommandID)
	require.Equal(t, repository.CommandStatusAcked, stored.Status)

	// 已终结后重复推进返回 false，且不改变状态。
	updated, err = repo.MarkAcked(ctx, phone, 0x8105, 31)
	require.NoError(t, err)
	require.False(t, updated)
	updated, err = repo.MarkFailed(ctx, phone, 0x8105, 31, "late failure")
	require.NoError(t, err)
	require.False(t, updated)

	stored, err = repo.FindByRouting(ctx, phone, 0x8105, 31)
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusAcked, stored.Status)
}

func TestCommandRepositoryMemoryPendingScanAndLease(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	repo := NewCommandRepositoryWithClock(clock.Now)
	phone := "188765470011"

	enqueue := func(id string, serial uint16, wait, expireAfter time.Duration) {
		_, created, err := repo.Enqueue(ctx, &repository.DownlinkCommand{
			CommandID:      id,
			IdempotencyKey: id,
			Phone:          phone,
			MsgID:          0x8105,
			Serial:         serial,
			Payload:        []byte{0x02},
		}, repository.CommandSchedule{InitialWait: wait, ExpireAfter: expireAfter})
		require.NoError(t, err)
		require.True(t, created)
	}
	enqueue("due-a", 21, -time.Minute, time.Hour)
	enqueue("due-b", 22, -time.Minute, time.Hour)
	enqueue("later", 23, time.Minute, time.Hour)

	// 只取到期的前 N 条，并支持分页继续取。
	firstPage, err := repo.ListPending(ctx, repository.PendingQuery{Limit: 1})
	require.NoError(t, err)
	require.Len(t, firstPage, 1)
	require.Equal(t, "due-a", firstPage[0].CommandID)
	secondPage, err := repo.ListPending(ctx, repository.PendingQuery{Limit: 1, Offset: 1})
	require.NoError(t, err)
	require.Len(t, secondPage, 1)
	require.Equal(t, "due-b", secondPage[0].CommandID)

	// 租约内其它实例不可重复占用。
	claimed, err := repo.ClaimPending(ctx, repository.CommandClaim{Owner: "node-a", Limit: 2, Lease: time.Minute})
	require.NoError(t, err)
	require.Len(t, claimed, 2)
	blocked, err := repo.ClaimPending(ctx, repository.CommandClaim{Owner: "node-b", Limit: 2, Lease: time.Minute})
	require.NoError(t, err)
	require.Empty(t, blocked)

	begun, err := repo.BeginAttempt(ctx, "due-a", "node-a")
	require.NoError(t, err)
	require.True(t, begun)
	begun, err = repo.BeginAttempt(ctx, "due-a", "node-b")
	require.NoError(t, err)
	require.False(t, begun)

	// 释放后回到 pending，并写入下一次可投时刻。
	released, err := repo.ReleaseAttempt(ctx, "due-a", "node-a", 30*time.Second, "send failed")
	require.NoError(t, err)
	require.True(t, released)

	// 租约到期后允许另一实例重新占用。
	clock.Advance(time.Minute + 2*time.Second)
	claimed, err = repo.ClaimPending(ctx, repository.CommandClaim{
		Owner: "node-b", Limit: 1, Lease: time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, "due-b", claimed[0].CommandID)

	stored, err := repo.Get(ctx, "due-a")
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusPending, stored.Status)
	require.Equal(t, 2, stored.Attempts)
	require.Equal(t, "send failed", stored.LastError)
	require.Empty(t, stored.LeaseOwner)
	require.Equal(t, now.Add(30*time.Second), stored.NextAttemptAt)
}

func TestCommandRepositoryMemoryExpirePending(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	repo := NewCommandRepositoryWithClock((&testClock{now: now}).Now)

	_, _, err := repo.Enqueue(ctx, &repository.DownlinkCommand{
		CommandID:      "expired",
		IdempotencyKey: "expired",
		Phone:          "188765470012",
		MsgID:          0x8105,
		Serial:         31,
	}, repository.CommandSchedule{InitialWait: -time.Hour, ExpireAfter: -time.Minute})
	require.NoError(t, err)
	_, _, err = repo.Enqueue(ctx, &repository.DownlinkCommand{
		CommandID:      "alive",
		IdempotencyKey: "alive",
		Phone:          "188765470012",
		MsgID:          0x8105,
		Serial:         32,
	}, repository.CommandSchedule{InitialWait: -time.Hour, ExpireAfter: time.Hour})
	require.NoError(t, err)

	expired, err := repo.ExpirePending(ctx, "expired before delivery")
	require.NoError(t, err)
	require.Equal(t, 1, expired)

	stored, err := repo.Get(ctx, "expired")
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusFailed, stored.Status)
	require.Equal(t, "expired before delivery", stored.LastError)

	stored, err = repo.Get(ctx, "alive")
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusPending, stored.Status)
}

// testClock 让内存仓储按测试推进的时间判定可投与过期。
type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time          { return c.now }
func (c *testClock) Advance(d time.Duration) { c.now = c.now.Add(d) }
