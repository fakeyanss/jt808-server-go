package mysql

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// MySQL 需要外部实例；未设置 JT808_TEST_MYSQL_DSN 时跳过。
// 数据库需先由 jt808-migrate 完成迁移，Open 只校验版本不执行 DDL。
func TestMySQLPersistence(t *testing.T) {
	dsn := os.Getenv("JT808_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("JT808_TEST_MYSQL_DSN is not set; skipping MySQL integration test")
	}
	ctx := context.Background()
	store, err := Open(dsn)
	require.NoError(t, err)
	require.NoError(t, store.DeviceRepository().DeleteByPhone(ctx, "188765420001"))

	device := &model.Device{
		ID:              "DEV-188765420001",
		Plate:           "京A42001",
		Phone:           "188765420001",
		SessionID:       "session-001",
		TransProto:      model.TCPProto,
		Keepalive:       time.Minute,
		LastestComTime:  time.Now(),
		Status:          model.DeviceStatusOnline,
		VersionDesc:     model.Version2019,
		AuthCode:        "auth-001",
		IMEI:            "IMEI-001",
		SoftwareVersion: "1.0.0",
	}
	require.NoError(t, store.DeviceRepository().Upsert(ctx, device))
	got, err := store.DeviceRepository().GetByPhone(ctx, device.Phone)
	require.NoError(t, err)
	require.Empty(t, got.SessionID, "runtime fields must not be persisted")
	require.True(t, got.LastestComTime.IsZero())

	params := &model.DeviceParams{DevicePhone: device.Phone, ParamCnt: 1, Params: []*model.ParamData{
		{ParamID: 0x0010, ParamLen: 4, ParamValue: "WXYZ"},
	}}
	require.NoError(t, store.DeviceParamsRepository().Replace(ctx, params))
	gotParams, err := store.DeviceParamsRepository().Get(ctx, device.Phone)
	require.NoError(t, err)
	require.Equal(t, "WXYZ", gotParams.Params[0].ParamValue)

	// 关闭后重开：运行时字段仍为零值，静态档案保留。
	require.NoError(t, store.Close())
	reopened, err := Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	got, err = reopened.DeviceRepository().GetByPhone(ctx, device.Phone)
	require.NoError(t, err)
	require.Equal(t, device.Phone, got.Phone)
	require.Empty(t, got.SessionID)
	require.Equal(t, model.DeviceStatusOffline, got.Status)
	require.True(t, got.LastestComTime.IsZero())
}

// 连接 DSN 必须显式打开 CLIENT_FOUND_ROWS，且保留原有参数。
func TestDSNWithMatchedRows(t *testing.T) {
	dsn, err := dsnWithMatchedRows("root:pass@tcp(127.0.0.1:3306)/jt808?parseTime=true&charset=utf8mb4")
	require.NoError(t, err)
	require.Contains(t, dsn, "clientFoundRows=true")
	require.Contains(t, dsn, "parseTime=true")
	require.Contains(t, dsn, "charset=utf8mb4")

	_, err = dsnWithMatchedRows("not-a-dsn")
	require.Error(t, err)
}
