package sqlstore

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

func openTestSQLite(t *testing.T) *Store {
	t.Helper()
	store, err := Open(Config{Backend: BackendSQLite, Path: filepath.Join(t.TempDir(), "jt808.db")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func sampleDevice(phone string) *model.Device {
	return &model.Device{
		ID:              "DEV-" + phone,
		Plate:           "京A" + phone[len(phone)-5:],
		Phone:           phone,
		SessionID:       "session-" + phone,
		TransProto:      model.TCPProto,
		Keepalive:       90 * time.Second,
		LastestComTime:  time.UnixMilli(1761700000123),
		Status:          model.DeviceStatusOnline,
		VersionDesc:     model.Version2019,
		ProtocolVersion: 1,
		AuthCode:        "auth-" + phone,
		IMEI:            "IMEI-" + phone,
		SoftwareVersion: "1.2.3",
	}
}

// SQLite 写入后关闭再打开同一文件，设备与参数仍在。
func TestSQLitePersistsDeviceAndParamsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "jt808.db")

	first, err := Open(Config{Backend: BackendSQLite, Path: dbPath})
	require.NoError(t, err)
	device := sampleDevice("188765400001")
	require.NoError(t, first.DeviceRepository().Upsert(ctx, device))
	params := &model.DeviceParams{
		DevicePhone: device.Phone,
		ParamCnt:    2,
		Params: []*model.ParamData{
			{ParamID: 0x0001, ParamLen: 4, ParamValue: uint32(700)},
			{ParamID: 0x0010, ParamLen: 8, ParamValue: "ABCDEFGH"},
		},
	}
	require.NoError(t, first.DeviceParamsRepository().Replace(ctx, params))
	require.NoError(t, first.Close())

	second, err := Open(Config{Backend: BackendSQLite, Path: dbPath})
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })

	got, err := second.DeviceRepository().GetByPhone(ctx, device.Phone)
	require.NoError(t, err)
	require.Equal(t, device.ID, got.ID)
	require.Equal(t, device.Plate, got.Plate)
	require.Equal(t, device.SessionID, got.SessionID)
	require.Equal(t, device.TransProto, got.TransProto)
	require.Equal(t, device.Keepalive, got.Keepalive)
	require.Equal(t, device.LastestComTime, got.LastestComTime)
	require.Equal(t, device.Status, got.Status)
	require.Equal(t, device.VersionDesc, got.VersionDesc)
	require.Equal(t, device.ProtocolVersion, got.ProtocolVersion)
	require.Equal(t, device.AuthCode, got.AuthCode)
	require.Equal(t, device.IMEI, got.IMEI)
	require.Equal(t, device.SoftwareVersion, got.SoftwareVersion)

	gotParams, err := second.DeviceParamsRepository().Get(ctx, device.Phone)
	require.NoError(t, err)
	require.Equal(t, device.Phone, gotParams.DevicePhone)
	require.Equal(t, params.ParamCnt, gotParams.ParamCnt)
	require.Len(t, gotParams.Params, 2)
	require.Equal(t, uint32(0x0001), gotParams.Params[0].ParamID)
	require.Equal(t, "ABCDEFGH", gotParams.Params[1].ParamValue)
}

// 属性档案：写入后内存可见；关闭再打开同一文件仍可读回；删除后返回既有错误值。
func TestSQLitePersistsTerminalProperties(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "jt808.db")

	first, err := Open(Config{Backend: BackendSQLite, Path: dbPath})
	require.NoError(t, err)
	props := &model.TerminalProperties{
		Phone:                   "188765400005",
		TerminalType:            0x1234,
		ManufacturerID:          "ACME",
		TerminalModel:           "T-2019",
		TerminalID:              "TERM-001",
		ICCID:                   "89860000000000000001",
		HardwareVersion:         "HW-1.0",
		FirmwareVersion:         "FW-2.3",
		GNSSProperties:          0x03,
		CommunicationProperties: 0x05,
		Extra:                   map[string]string{"legacy": "value"},
	}
	repo := first.TerminalPropertiesRepository()
	require.NoError(t, repo.Replace(ctx, props))

	// 同进程内直接命中内存最新值，不依赖回源。
	cached, err := repo.Get(ctx, props.Phone)
	require.NoError(t, err)
	require.Equal(t, props.TerminalModel, cached.TerminalModel)
	cached.Extra["legacy"] = "mutated"
	again, err := repo.Get(ctx, props.Phone)
	require.NoError(t, err)
	require.Equal(t, "value", again.Extra["legacy"])

	require.NoError(t, first.Close())
	second, err := Open(Config{Backend: BackendSQLite, Path: dbPath})
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })

	got, err := second.TerminalPropertiesRepository().Get(ctx, props.Phone)
	require.NoError(t, err)
	require.Equal(t, props.TerminalType, got.TerminalType)
	require.Equal(t, props.ManufacturerID, got.ManufacturerID)
	require.Equal(t, props.TerminalID, got.TerminalID)
	require.Equal(t, props.ICCID, got.ICCID)
	require.Equal(t, props.HardwareVersion, got.HardwareVersion)
	require.Equal(t, props.FirmwareVersion, got.FirmwareVersion)
	require.Equal(t, props.GNSSProperties, got.GNSSProperties)
	require.Equal(t, props.CommunicationProperties, got.CommunicationProperties)
	require.Equal(t, "value", got.Extra["legacy"])

	require.NoError(t, second.TerminalPropertiesRepository().Delete(ctx, props.Phone))
	_, err = second.TerminalPropertiesRepository().Get(ctx, props.Phone)
	require.ErrorIs(t, err, repository.ErrTerminalPropertiesNotFound)
}

func TestSQLiteNotFoundAndDeleteSemantics(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)

	_, err := store.DeviceRepository().GetByPhone(ctx, "188765400002")
	require.ErrorIs(t, err, repository.ErrDeviceNotFound)
	_, err = store.DeviceRepository().GetByPlate(ctx, "京A00000")
	require.ErrorIs(t, err, repository.ErrDeviceNotFound)
	_, err = store.DeviceParamsRepository().Get(ctx, "188765400002")
	require.ErrorIs(t, err, repository.ErrDeviceParamsNotFound)

	device := sampleDevice("188765400003")
	require.NoError(t, store.DeviceRepository().Upsert(ctx, device))
	require.NoError(t, store.DeviceRepository().DeleteByPhone(ctx, device.Phone))
	_, err = store.DeviceRepository().GetByPhone(ctx, device.Phone)
	require.ErrorIs(t, err, repository.ErrDeviceNotFound)

	other := sampleDevice("188765400004")
	other.Plate = device.Plate
	require.NoError(t, store.DeviceRepository().Upsert(ctx, other))
	require.NoError(t, store.DeviceRepository().DeleteByPlate(ctx, other.Plate))
	_, err = store.DeviceRepository().GetByPhone(ctx, other.Phone)
	require.ErrorIs(t, err, repository.ErrDeviceNotFound)
}

func TestSQLiteConcurrentDeviceWrites(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)

	const workers, perWorker = 8, 10
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				phone := []string{
					"188765410001", "188765410002", "188765410003", "188765410004",
					"188765410005", "188765410006", "188765410007", "188765410008",
				}[worker]
				assert.NoError(t, store.DeviceRepository().Upsert(ctx, sampleDevice(phone)))
				_, err := store.DeviceRepository().GetByPhone(ctx, phone)
				assert.NoError(t, err)
			}
		}(w)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			devices, err := store.DeviceRepository().List(ctx)
			assert.NoError(t, err)
			assert.LessOrEqual(t, len(devices), workers)
		}
	}()
	wg.Wait()

	devices, err := store.DeviceRepository().List(ctx)
	require.NoError(t, err)
	require.Len(t, devices, workers)
}

// MySQL 需要外部实例；未设置 JT808_TEST_MYSQL_DSN 时跳过。
func TestMySQLPersistence(t *testing.T) {
	dsn := os.Getenv("JT808_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("JT808_TEST_MYSQL_DSN is not set; skipping MySQL integration test")
	}
	ctx := context.Background()
	store, err := Open(Config{Backend: BackendMySQL, DSN: dsn})
	require.NoError(t, err)
	require.NoError(t, store.DeviceRepository().DeleteByPhone(ctx, "188765420001"))
	require.NoError(t, store.DeviceRepository().DeleteByPhone(ctx, "188765420002"))

	device := sampleDevice("188765420001")
	require.NoError(t, store.DeviceRepository().Upsert(ctx, device))
	got, err := store.DeviceRepository().GetByPhone(ctx, device.Phone)
	require.NoError(t, err)
	require.Equal(t, device.SessionID, got.SessionID)
	require.Equal(t, device.LastestComTime, got.LastestComTime)

	params := &model.DeviceParams{DevicePhone: device.Phone, ParamCnt: 1, Params: []*model.ParamData{
		{ParamID: 0x0010, ParamLen: 4, ParamValue: "WXYZ"},
	}}
	require.NoError(t, store.DeviceParamsRepository().Replace(ctx, params))
	gotParams, err := store.DeviceParamsRepository().Get(ctx, device.Phone)
	require.NoError(t, err)
	require.Equal(t, "WXYZ", gotParams.Params[0].ParamValue)

	require.NoError(t, store.Close())
	reopened, err := Open(Config{Backend: BackendMySQL, DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	got, err = reopened.DeviceRepository().GetByPhone(ctx, device.Phone)
	require.NoError(t, err)
	require.Equal(t, device.Phone, got.Phone)
}
