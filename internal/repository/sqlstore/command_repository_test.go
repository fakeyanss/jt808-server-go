package sqlstore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

func TestSQLiteCommandOutboxIdempotencyAndResult(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "jt808.db")
	migrateSQLite(t, dbPath)
	store, err := Open(Config{Backend: BackendSQLite, Path: dbPath})
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

	reopened, err := Open(Config{Backend: BackendSQLite, Path: dbPath})
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	persisted, err := reopened.CommandRepository().Get(ctx, "cmd-sql-1")
	require.NoError(t, err)
	require.Equal(t, repository.CommandStatusAcked, persisted.Status)
	require.Equal(t, phone, persisted.Phone)
}
