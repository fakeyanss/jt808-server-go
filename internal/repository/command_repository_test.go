package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCommandRepositoryMemoryIdempotencyAndResult(t *testing.T) {
	ctx := context.Background()
	repo := NewCommandRepository()

	first, created, err := repo.Enqueue(ctx, &DownlinkCommand{
		CommandID:      "cmd-1",
		IdempotencyKey: "idem-1",
		Phone:          "188765470001",
		MsgID:          0x8105,
		Serial:         7,
		Payload:        []byte{0x02},
	})
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, CommandStatusPending, first.Status)
	require.Equal(t, 1, first.Attempts)

	// 同设备同幂等键返回已有记录，不覆盖原 commandId。
	duplicate, created, err := repo.Enqueue(ctx, &DownlinkCommand{
		CommandID:      "cmd-2",
		IdempotencyKey: "idem-1",
		Phone:          "188765470001",
		MsgID:          0x8105,
		Serial:         8,
	})
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, "cmd-1", duplicate.CommandID)
	require.Equal(t, uint16(7), duplicate.Serial)

	// 不同设备复用同一幂等键互不影响。
	otherPhone, created, err := repo.Enqueue(ctx, &DownlinkCommand{
		CommandID:      "cmd-3",
		IdempotencyKey: "idem-1",
		Phone:          "188765470002",
		MsgID:          0x8105,
		Serial:         9,
	})
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
	require.Equal(t, CommandStatusAcked, stored.Status)
	require.Empty(t, stored.LastError)

	failed, err := repo.MarkFailed(ctx, "188765470002", 0x8105, 9, "retransmission exhausted")
	require.NoError(t, err)
	require.True(t, failed)
	stored, err = repo.Get(ctx, "cmd-3")
	require.NoError(t, err)
	require.Equal(t, CommandStatusFailed, stored.Status)
	require.Equal(t, "retransmission exhausted", stored.LastError)

	_, err = repo.Get(ctx, "missing")
	require.ErrorIs(t, err, ErrCommandNotFound)
}

func TestCommandRepositoryMemoryPendingScanAndLease(t *testing.T) {
	ctx := context.Background()
	repo := NewCommandRepository()
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	phone := "188765470011"

	enqueue := func(id string, serial uint16, next, expires time.Time) {
		_, created, err := repo.Enqueue(ctx, &DownlinkCommand{
			CommandID:      id,
			IdempotencyKey: id,
			Phone:          phone,
			MsgID:          0x8105,
			Serial:         serial,
			Payload:        []byte{0x02},
			NextAttemptAt:  next,
			ExpiresAt:      expires,
		})
		require.NoError(t, err)
		require.True(t, created)
	}
	enqueue("due-a", 21, now.Add(-time.Minute), now.Add(time.Hour))
	enqueue("due-b", 22, now.Add(-time.Minute), now.Add(time.Hour))
	enqueue("later", 23, now.Add(time.Minute), now.Add(time.Hour))

	// 只取到期的前 N 条，并支持分页继续取。
	firstPage, err := repo.ListPending(ctx, PendingQuery{DueBefore: now, Limit: 1})
	require.NoError(t, err)
	require.Len(t, firstPage, 1)
	require.Equal(t, "due-a", firstPage[0].CommandID)
	secondPage, err := repo.ListPending(ctx, PendingQuery{DueBefore: now, Limit: 1, Offset: 1})
	require.NoError(t, err)
	require.Len(t, secondPage, 1)
	require.Equal(t, "due-b", secondPage[0].CommandID)

	// 租约内其它实例不可重复占用。
	claimed, err := repo.ClaimPending(ctx, CommandClaim{Owner: "node-a", Now: now, Limit: 2, Lease: time.Minute})
	require.NoError(t, err)
	require.Len(t, claimed, 2)
	blocked, err := repo.ClaimPending(ctx, CommandClaim{Owner: "node-b", Now: now, Limit: 2, Lease: time.Minute})
	require.NoError(t, err)
	require.Empty(t, blocked)

	begun, err := repo.BeginAttempt(ctx, "due-a", "node-a", now)
	require.NoError(t, err)
	require.True(t, begun)
	begun, err = repo.BeginAttempt(ctx, "due-a", "node-b", now)
	require.NoError(t, err)
	require.False(t, begun)

	// 释放后回到 pending，并写入下一次可投时刻。
	released, err := repo.ReleaseAttempt(ctx, "due-a", "node-a", now.Add(30*time.Second), "device offline")
	require.NoError(t, err)
	require.True(t, released)

	// 租约到期后允许另一实例重新占用。
	claimed, err = repo.ClaimPending(ctx, CommandClaim{
		Owner: "node-b", Now: now.Add(time.Minute + 2*time.Second), Limit: 1, Lease: time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, "due-b", claimed[0].CommandID)

	stored, err := repo.Get(ctx, "due-a")
	require.NoError(t, err)
	require.Equal(t, CommandStatusPending, stored.Status)
	require.Equal(t, 2, stored.Attempts)
	require.Equal(t, "device offline", stored.LastError)
	require.Empty(t, stored.LeaseOwner)
	require.Equal(t, now.Add(30*time.Second), stored.NextAttemptAt)
}

func TestCommandRepositoryMemoryExpirePending(t *testing.T) {
	ctx := context.Background()
	repo := NewCommandRepository()
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

	_, _, err := repo.Enqueue(ctx, &DownlinkCommand{
		CommandID:      "expired",
		IdempotencyKey: "expired",
		Phone:          "188765470012",
		MsgID:          0x8105,
		Serial:         31,
		NextAttemptAt:  now.Add(-time.Hour),
		ExpiresAt:      now.Add(-time.Minute),
	})
	require.NoError(t, err)
	_, _, err = repo.Enqueue(ctx, &DownlinkCommand{
		CommandID:      "alive",
		IdempotencyKey: "alive",
		Phone:          "188765470012",
		MsgID:          0x8105,
		Serial:         32,
		NextAttemptAt:  now.Add(-time.Hour),
		ExpiresAt:      now.Add(time.Hour),
	})
	require.NoError(t, err)

	expired, err := repo.ExpirePending(ctx, now, "expired before delivery")
	require.NoError(t, err)
	require.Equal(t, 1, expired)

	stored, err := repo.Get(ctx, "expired")
	require.NoError(t, err)
	require.Equal(t, CommandStatusFailed, stored.Status)
	require.Equal(t, "expired before delivery", stored.LastError)

	stored, err = repo.Get(ctx, "alive")
	require.NoError(t, err)
	require.Equal(t, CommandStatusPending, stored.Status)
}
