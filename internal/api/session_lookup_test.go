package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

// 回归：SQL 后端不持久化运行时字段，Device.SessionID 读回为空。
// 下行会话定位必须走运行时注册表的手机号索引，按持久化 SessionID 查一定失败。
func TestLookupSessionByPhoneIgnoresPersistedSessionID(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "jt808.db")
	_, err := storage.Migrate(storage.Config{Type: storage.BackendSQLite, SQLitePath: dbPath})
	require.NoError(t, err)

	stores, err := storage.Open(storage.Config{Type: storage.BackendSQLite, SQLitePath: dbPath})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stores.Close()) })

	const phone = "18800001111"
	require.NoError(t, stores.Devices.Upsert(ctx, &model.Device{
		Phone:     phone,
		Plate:     "测A12345",
		Keepalive: time.Minute,
	}))

	device, err := stores.Devices.GetByPhone(ctx, phone)
	require.NoError(t, err)
	require.Empty(t, device.SessionID, "SQL 后端不应持久化 SessionID")

	conn := runtime.NewConnectionRegistry()
	session := &model.Session{ID: "session-by-phone", Transport: model.TCPProto}
	require.NoError(t, conn.Bind(ctx, session))
	_, err = conn.BindPhone(ctx, phone, session.ID)
	require.NoError(t, err)

	got, err := lookupSessionByPhone(ctx, conn, device)
	require.NoError(t, err)
	require.Equal(t, session.ID, got.ID)

	// 旧实现按持久化 SessionID 查找，SQL 后端下必然落到 ErrSessionClosed。
	_, err = conn.Get(ctx, device.SessionID)
	require.ErrorIs(t, err, runtime.ErrSessionClosed)
}
