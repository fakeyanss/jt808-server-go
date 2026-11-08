package repository

import (
	"context"
	"testing"

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
