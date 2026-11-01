package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

func TestOpenRejectsInvalidConfig(t *testing.T) {
	_, err := Open(Config{Type: "redis"})
	require.Error(t, err)

	_, err = Open(Config{Type: BackendSQLite})
	require.Error(t, err)

	_, err = Open(Config{Type: BackendMySQL})
	require.Error(t, err)
}

func TestOpenDefaultsToMemory(t *testing.T) {
	stores, err := Open(Config{})
	require.NoError(t, err)
	require.Equal(t, BackendMemory, stores.Backend)
	require.NoError(t, stores.Close())
}

func TestOpenSQLitePersistsDevicesAndParams(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "jt808.db")

	_, err := Migrate(Config{Type: BackendSQLite, SQLitePath: dbPath})
	require.NoError(t, err)

	first, err := Open(Config{Type: BackendSQLite, SQLitePath: dbPath})
	require.NoError(t, err)
	require.Equal(t, BackendSQLite, first.Backend)

	phone := "188765430001"
	device := &model.Device{
		ID:             "DEV-001",
		Plate:          "京A12345",
		Phone:          phone,
		SessionID:      "session-001",
		TransProto:     model.TCPProto,
		Keepalive:      time.Minute,
		LastestComTime: time.UnixMilli(1761700000000),
		Status:         model.DeviceStatusOnline,
		VersionDesc:    model.Version2013,
	}
	require.NoError(t, first.Devices.Upsert(ctx, device))
	params := &model.DeviceParams{DevicePhone: phone, ParamCnt: 1, Params: []*model.ParamData{
		{ParamID: 0x0010, ParamLen: 4, ParamValue: "TEST"},
	}}
	require.NoError(t, first.Params.Replace(ctx, params))
	require.NoError(t, first.Close())

	second, err := Open(Config{Type: BackendSQLite, SQLitePath: dbPath})
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })

	got, err := second.Devices.GetByPhone(ctx, phone)
	require.NoError(t, err)
	require.Equal(t, device.ID, got.ID)
	require.Equal(t, device.LastestComTime, got.LastestComTime)
	gone := "188765430099"
	_, err = second.Devices.GetByPhone(ctx, gone)
	require.ErrorIs(t, err, repository.ErrDeviceNotFound)

	gotParams, err := second.Params.Get(ctx, phone)
	require.NoError(t, err)
	require.Equal(t, "TEST", gotParams.Params[0].ParamValue)
}

// Migrate 是独立入口，可重复执行且不初始化进程默认存储。
func TestMigrateSQLiteIsRepeatable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "jt808.db")

	applied, err := Migrate(Config{Type: BackendSQLite, SQLitePath: dbPath})
	require.NoError(t, err)
	require.Greater(t, applied, 0)

	again, err := Migrate(Config{Type: BackendSQLite, SQLitePath: dbPath})
	require.NoError(t, err)
	require.Zero(t, again)
}

// 内存后端没有 schema，迁移命令应给出明确错误。
func TestMigrateRejectsUnsupportedTargets(t *testing.T) {
	_, err := Migrate(Config{Type: BackendMemory})
	require.Error(t, err)

	_, err = Migrate(Config{Type: "redis"})
	require.Error(t, err)
}
