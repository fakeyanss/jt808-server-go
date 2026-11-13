package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

func TestSQLiteCommandOutboxIdempotencyAndResult(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "jt808.db")
	migrateSQLite(t, dbPath)
	store, err := Open(dbPath, 0)
	require.NoError(t, err)
	commands := store.CommandRepository()

	const phone = "188765477001"
	first, created, err := commands.Enqueue(ctx, &repository.DownlinkCommand{
		CommandID:      "cmd-sql-1",
		IdempotencyKey: "idem-sql-1",
		Phone:          phone,
		MsgID:          0x8105,
		Serial:         11,
		Payload:        []byte{0x02, 0x01},
	})
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, repository.CommandStatusPending, first.Status)
	require.Equal(t, []byte{0x02, 0x01}, first.Payload)

	duplicate, created, err := commands.Enqueue(ctx, &repository.DownlinkCommand{
		CommandID:      "cmd-sql-2",
		IdempotencyKey: "idem-sql-1",
		Phone:          phone,
		MsgID:          0x8105,
		Serial:         12,
	})
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, "cmd-sql-1", duplicate.CommandID)
	require.Equal(t, uint16(11), duplicate.Serial)

	updated, err := commands.MarkAcked(ctx, phone, 0x8105, 11)
	require.NoError(t, err)
	require.True(t, updated)

	stored, err := commands.Get(ctx, "cmd-sql-1")
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusAcked, stored.Status)

	_, err = commands.Get(ctx, "missing")
	require.ErrorIs(t, err, repository.ErrCommandNotFound)

	require.NoError(t, store.Close())

	reopened, err := Open(dbPath, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	persisted, err := reopened.CommandRepository().Get(ctx, "cmd-sql-1")
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusAcked, persisted.Status)
	require.Equal(t, phone, persisted.Phone)
}

func TestSQLiteCommandOutboxReplayLifecycle(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "jt808-replay.db")
	migrateSQLite(t, dbPath)
	store, err := Open(dbPath, 0)
	require.NoError(t, err)
	commands := store.CommandRepository()

	const phone = "188765477002"
	now := time.Now().UTC().Truncate(time.Millisecond)
	enqueue := func(id string, serial uint16, next, expires time.Time) {
		_, created, err := commands.Enqueue(ctx, &repository.DownlinkCommand{
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
	enqueue("due", 41, now.Add(-time.Minute), now.Add(time.Hour))
	enqueue("later", 42, now.Add(time.Minute), now.Add(time.Hour))
	enqueue("expired", 43, now.Add(-time.Minute), now.Add(-time.Minute))

	pending, err := commands.ListPending(ctx, repository.PendingQuery{DueBefore: now, Limit: 10})
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, "due", pending[0].CommandID)
	require.True(t, pending[0].NextAttemptAt.Equal(now.Add(-time.Minute)))
	require.True(t, pending[0].ExpiresAt.Equal(now.Add(time.Hour)))

	claimed, err := commands.ClaimPending(ctx, repository.CommandClaim{
		Owner: "node-a", Now: now, Limit: 10, Lease: time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, "due", claimed[0].CommandID)
	require.Equal(t, "node-a", claimed[0].LeaseOwner)
	require.True(t, claimed[0].LeaseExpiresAt.Equal(now.Add(time.Minute)))

	blocked, err := commands.ClaimPending(ctx, repository.CommandClaim{
		Owner: "node-b", Now: now, Limit: 10, Lease: time.Minute,
	})
	require.NoError(t, err)
	require.Empty(t, blocked)

	begun, err := commands.BeginAttempt(ctx, "due", "node-a", now)
	require.NoError(t, err)
	require.True(t, begun)
	begun, err = commands.BeginAttempt(ctx, "due", "node-b", now)
	require.NoError(t, err)
	require.False(t, begun)

	stored, err := commands.Get(ctx, "due")
	require.NoError(t, err)
	require.Equal(t, 2, stored.Attempts)
	require.True(t, stored.LastAttemptAt.Equal(now))

	expired, err := commands.ExpirePending(ctx, now, "expired before delivery")
	require.NoError(t, err)
	require.Equal(t, 1, expired)

	updated, err := commands.MarkAcked(ctx, phone, 0x8105, 41)
	require.NoError(t, err)
	require.True(t, updated)
	stored, err = commands.Get(ctx, "due")
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusAcked, stored.Status)
	require.Empty(t, stored.LeaseOwner)

	require.NoError(t, store.Close())

	// 重启后重投所需的状态必须完整保留。
	reopened, err := Open(dbPath, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	persisted, err := reopened.CommandRepository().Get(ctx, "due")
	require.NoError(t, err)
	require.Equal(t, 2, persisted.Attempts)
	require.True(t, persisted.LastAttemptAt.Equal(now))
	require.True(t, persisted.ExpiresAt.Equal(now.Add(time.Hour)))
	expiredCommand, err := reopened.CommandRepository().Get(ctx, "expired")
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusFailed, expiredCommand.Status)
	require.Equal(t, "expired before delivery", expiredCommand.LastError)
}

// TestSQLiteCommandNextSerialSkipsInFlightAfterWrap 覆盖回绕：
// 游标自增跨越 65535 后回到 1，必须跳过同 msgId 仍未终结的组合；
// 不同 msgId 复用同一 serial 是安全的。
func TestSQLiteCommandNextSerialSkipsInFlightAfterWrap(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "jt808-serial.db")
	migrateSQLite(t, dbPath)
	store, err := Open(dbPath, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	commands := store.CommandRepository()

	const phone = "188765477003"
	first, err := commands.NextSerial(ctx, phone, 0x8105)
	require.NoError(t, err)
	require.Equal(t, uint16(1), first)

	// 同 msgId 的 serial=1 仍在途。
	_, created, err := commands.Enqueue(ctx, &repository.DownlinkCommand{
		CommandID: "wrap-in-flight", IdempotencyKey: "wrap-in-flight",
		Phone: phone, MsgID: 0x8105, Serial: 1, Payload: []byte{0x02},
	})
	require.NoError(t, err)
	require.True(t, created)

	// 把游标拨到 65535，模拟长时间运行后的回绕。
	_, err = store.DB().ExecContext(ctx, `UPDATE command_serial SET next_serial = 65535 WHERE phone = ?`, phone)
	require.NoError(t, err)
	wrapped, err := commands.NextSerial(ctx, phone, 0x8105)
	require.NoError(t, err)
	require.Equal(t, uint16(65535), wrapped)

	afterWrap, err := commands.NextSerial(ctx, phone, 0x8105)
	require.NoError(t, err)
	require.Equal(t, uint16(2), afterWrap, "回绕后必须跳过同 msgId 的在途 serial")

	// 不同 msgId 复用 serial=1 安全。
	_, err = store.DB().ExecContext(ctx, `UPDATE command_serial SET next_serial = 1 WHERE phone = ?`, phone)
	require.NoError(t, err)
	reused, err := commands.NextSerial(ctx, phone, 0x8608)
	require.NoError(t, err)
	require.Equal(t, uint16(1), reused)
}

// TestSQLiteCommandReleaseOfflineRestoresAttempts 覆盖离线释放：
// 租约清空、attempts 与 last_attempt_at 回到占用前快照，NextAttemptAt 不变。
func TestSQLiteCommandReleaseOfflineRestoresAttempts(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "jt808-offline.db")
	migrateSQLite(t, dbPath)
	store, err := Open(dbPath, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	commands := store.CommandRepository()

	const phone = "188765477004"
	now := time.Now().UTC().Truncate(time.Millisecond)
	_, created, err := commands.Enqueue(ctx, &repository.DownlinkCommand{
		CommandID: "offline", IdempotencyKey: "offline", Phone: phone,
		MsgID: 0x8105, Serial: 7, Payload: []byte{0x02},
		NextAttemptAt: now, ExpiresAt: now.Add(time.Hour),
	})
	require.NoError(t, err)
	require.True(t, created)

	_, err = commands.ClaimPending(ctx, repository.CommandClaim{
		Owner: "node-a", Now: now, Limit: 1, Lease: time.Minute,
	})
	require.NoError(t, err)
	begun, err := commands.BeginAttempt(ctx, "offline", "node-a", now)
	require.NoError(t, err)
	require.True(t, begun)

	next := now.Add(15 * time.Second)
	released, err := commands.ReleaseOffline(ctx, "offline", "node-a", 1, time.Time{}, next, "device offline")
	require.NoError(t, err)
	require.True(t, released)

	stored, err := commands.Get(ctx, "offline")
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusPending, stored.Status)
	require.Equal(t, 1, stored.Attempts)
	require.True(t, stored.LastAttemptAt.IsZero())
	require.Empty(t, stored.LeaseOwner)
	require.Equal(t, "device offline", stored.LastError)
	require.True(t, stored.NextAttemptAt.Equal(next))
}
