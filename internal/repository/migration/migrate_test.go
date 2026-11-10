package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	// 内存库按连接隔离，限定单连接避免读取到另一份库。
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestExpectedVersionMatchesEmbeddedFiles(t *testing.T) {
	// 开发阶段按批次追加版本；引导脚本不参与版本编号。
	require.Len(t, migrations, 12)
	require.Equal(t, "20261101_init.sql", migrations[0].name)
	require.Equal(t, "20261102_persistence_tables.sql", migrations[1].name)
	require.Equal(t, "20261105_drop_device_runtime.sql", migrations[2].name)
	require.Equal(t, "20261108_media_metadata.sql", migrations[3].name)
	require.Equal(t, "20261109_command_outbox.sql", migrations[4].name)
	require.Equal(t, "20261110_command_outbox_replay.sql", migrations[5].name)
	require.Equal(t, "20261111_upgrade_results.sql", migrations[6].name)
	require.Equal(t, "20261112_media_files.sql", migrations[7].name)
	require.Equal(t, "20261113_bcd_time_gmt8.sql", migrations[8].name)
	require.Equal(t, "20261114_media_search_results.sql", migrations[9].name)
	require.Equal(t, "20261115_retention_indexes.sql", migrations[10].name)
	require.Equal(t, "20261116_locations_dedup_key.sql", migrations[11].name)
	require.Equal(t, 20261116, ExpectedVersion())
}

// 同一库重复执行迁移只应用一次，版本记录不重复。
func TestApplyIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	applied, err := Apply(ctx, db)
	require.NoError(t, err)
	require.Equal(t, len(migrations), applied)

	again, err := Apply(ctx, db)
	require.NoError(t, err)
	require.Equal(t, 0, again)

	var rows int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&rows))
	require.Equal(t, len(migrations), rows)

	// 迁移后业务表与车牌索引均存在；device_runtime 已在最后一个版本中删除。
	for _, table := range []string{
		"devices", "device_params", "terminal_properties",
		"terminal_catalogs", "region_items", "locations", "alarms",
		"media_resource_lists", "media_upload_tasks", "media_upload_notices",
		"media_files", "media_captures", "media_capture_items", "media_search_results",
		"upgrade_results",
	} {
		var name string
		err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		require.NoError(t, err, "table %s should exist", table)
	}
	var index string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'idx_devices_plate'`).Scan(&index))
	require.Equal(t, "idx_devices_plate", index)
}

// 最终 schema 不再包含 device_runtime；运行时状态由进程内注册表持有。
func TestApplyDropsDeviceRuntime(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	_, err := Apply(ctx, db)
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'device_runtime'`).Scan(&count))
	require.Zero(t, count)

	// devices 仍只有静态列，运行时列不再落库。
	for _, col := range []string{"session_id", "trans_proto", "last_com_time_ms", "status"} {
		require.NoError(t, db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pragma_table_info('devices') WHERE name = ?`, col).Scan(&count))
		require.Zero(t, count, "devices should not contain %s", col)
	}
}

func TestSplitStatements(t *testing.T) {
	script := `
-- 注释中的分号不切分
CREATE TABLE a (id INTEGER);
INSERT INTO a VALUES (1);
INSERT INTO a VALUES ('x;y');
`
	stmts := splitStatements(script)
	require.Len(t, stmts, 3)
	require.Equal(t, "CREATE TABLE a (id INTEGER)", stmts[0])
	require.Equal(t, "INSERT INTO a VALUES (1)", stmts[1])
	require.Equal(t, "INSERT INTO a VALUES ('x;y')", stmts[2])
}

func TestParseVersionUsesDatePrefix(t *testing.T) {
	version, err := parseVersion("20261101_init.sql")
	require.NoError(t, err)
	require.Equal(t, 20261101, version)

	_, err = parseVersion("init.sql")
	require.Error(t, err)

	_, err = parseVersion("20261101.sql")
	require.Error(t, err)

	_, err = parseVersion("2026111_init.sql")
	require.Error(t, err)

	_, err = parseVersion("20261301_init.sql")
	require.Error(t, err)
}

// 老库升级路径：已应用 20261101/20261102 的库再次执行迁移时只追加 20261105；
// device_runtime 被删除，devices 中的静态档案保留。
func TestApplyUpgradesLegacyDatabase(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	for _, stmt := range bootstrapStatements {
		_, err := db.ExecContext(ctx, stmt)
		require.NoError(t, err)
	}
	for _, m := range migrations[:2] {
		for _, stmt := range m.stmts {
			_, err := db.ExecContext(ctx, stmt)
			require.NoError(t, err)
		}
		_, err := db.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, 0)`, m.version)
		require.NoError(t, err)
	}

	// 旧 schema 下写入一份设备静态档案与一条运行时投影。
	_, err := db.ExecContext(ctx,
		`INSERT INTO devices (phone, id, plate, keepalive_ms, version_desc, protocol_version,
			auth_code, imei, software_version, updated_at)
		 VALUES ('13800000001', 'DEV-UPGRADE', '京A12345', 60000, 1, 1, 'AUTH', 'IMEI', 'SW', 1)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO device_runtime (phone, session_id, trans_proto, last_com_time_ms, status, updated_at)
		 VALUES ('13800000001', 'old-session', 'tcp', 100, 1, 1)`)
	require.NoError(t, err)

	current, err := CurrentVersion(ctx, db)
	require.NoError(t, err)
	require.Equal(t, 20261102, current)

	applied, err := Apply(ctx, db)
	require.NoError(t, err)
	require.Equal(t, len(migrations)-2, applied)
	require.Equal(t, ExpectedVersion(), mustCurrentVersion(t, ctx, db))

	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'device_runtime'`).Scan(&count))
	require.Zero(t, count, "device_runtime should be dropped")

	var plate string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT plate FROM devices WHERE phone = '13800000001'`).Scan(&plate))
	require.Equal(t, "京A12345", plate)
}

func mustCurrentVersion(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	current, err := CurrentVersion(ctx, db)
	require.NoError(t, err)
	return current
}

// 迁移正确性：旧的“伪 UTC”终端时间统一减 8 小时，
// 服务端接收时间与平台下发参数保持原值。
func TestApplyShiftsBCDTimesToGMT8(t *testing.T) {
	const shiftMS = int64(8 * 60 * 60 * 1000)
	ctx := context.Background()
	db := newTestDB(t)

	// 只应用到 20261113 之前，模拟尚未执行时区修正的旧库。
	bcdIndex := -1
	for index, m := range migrations {
		if m.name == "20261113_bcd_time_gmt8.sql" {
			bcdIndex = index
			break
		}
	}
	require.GreaterOrEqual(t, bcdIndex, 0)
	for _, stmt := range bootstrapStatements {
		_, err := db.ExecContext(ctx, stmt)
		require.NoError(t, err)
	}
	for _, m := range migrations[:bcdIndex] {
		for _, stmt := range m.stmts {
			_, err := db.ExecContext(ctx, stmt)
			require.NoError(t, err)
		}
		_, err := db.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, 0)`, m.version)
		require.NoError(t, err)
	}

	locationTime := time.Date(2026, 10, 28, 20, 45, 0, 0, time.UTC)
	locationReceived := locationTime.Add(3 * time.Second)
	_, err := db.ExecContext(ctx,
		`INSERT INTO locations (phone, record_id, msg_id, report_time_ms, received_at_ms,
		 alarm_sign, status_sign, latitude, longitude, altitude, speed, direction, payload_json)
		 VALUES ('188765000001', 1, 512, ?, ?, 0, 0, 0, 0, 0, 0, 0, '{"occurredAt":"261028204500"}')`,
		locationTime.UnixMilli(), locationReceived.UnixMilli())
	require.NoError(t, err)

	alarmOccurred := time.Date(2026, 10, 28, 21, 0, 0, 0, time.UTC)
	alarmReceived := alarmOccurred.Add(2 * time.Second)
	for _, row := range []struct {
		serial   int
		occurred int64
		received int64
	}{
		{1, alarmOccurred.UnixMilli(), alarmReceived.UnixMilli()},
		{2, 0, alarmReceived.Add(time.Second).UnixMilli()},
	} {
		_, err := db.ExecContext(ctx,
			`INSERT INTO alarms (phone, message_serial, received_at_ms, occurred_at_ms,
			 alarm_sign, latitude, longitude, confirmed) VALUES ('188765000001', ?, ?, ?, 1, 0, 0, 0)`,
			row.serial, row.received, row.occurred)
		require.NoError(t, err)
	}

	mediaTime := time.Date(2026, 11, 12, 10, 30, 0, 0, time.UTC)
	mediaReceived := mediaTime.Add(5 * time.Second)
	_, err = db.ExecContext(ctx,
		`INSERT INTO media_files (phone, media_id, location_time, relative_path, received_at)
		 VALUES ('188765000001', 9001, ?, '188765000001/20261112/9001.jpg', ?)`,
		mediaTime.UnixMilli(), mediaReceived.UnixMilli())
	require.NoError(t, err)

	// 1078 资源列表的终端时间内嵌在 JSON 里：原地平移，不清空；
	// 平台下发的 0x9206 参数不是终端 BCD 派生，必须原样保留。
	const resourceItemsJSON = `[{"logicChannelId":1,"startTime":"2026-11-12T10:30:00Z","endTime":"2026-11-12T10:31:00Z","alarmSign":0,"alarmSignExt":0,"mediaType":2,"streamType":1,"storageType":1,"size":123456},{"logicChannelId":2,"startTime":null,"endTime":null,"alarmSign":0,"alarmSignExt":0,"mediaType":1,"streamType":0,"storageType":1,"size":789}]`
	_, err = db.ExecContext(ctx,
		`INSERT INTO media_resource_lists (phone, media_count, items_json, updated_at)
		 VALUES ('188765000001', 2, ?, 1)`, resourceItemsJSON)
	require.NoError(t, err)
	const taskPayload = `{"startTime":"2026-11-12T10:30:00Z"}`
	_, err = db.ExecContext(ctx,
		`INSERT INTO media_upload_tasks (phone, serial, payload_json, updated_at)
		 VALUES ('188765000001', 7, ?, 1)`, taskPayload)
	require.NoError(t, err)

	applied, err := Apply(ctx, db)
	require.NoError(t, err)
	require.Equal(t, len(migrations)-bcdIndex, applied)
	require.Equal(t, ExpectedVersion(), mustCurrentVersion(t, ctx, db))

	var reportMS, receivedMS int64
	var locationPayload string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT report_time_ms, received_at_ms, payload_json FROM locations WHERE phone = '188765000001'`).
		Scan(&reportMS, &receivedMS, &locationPayload))
	require.Equal(t, locationTime.UnixMilli()-shiftMS, reportMS)
	require.Equal(t, locationReceived.UnixMilli(), receivedMS, "received time must not be shifted")
	require.Equal(t, `{"occurredAt":"261028204500"}`, locationPayload, "raw BCD payload must not be rewritten")
	require.Equal(t, "261028204500",
		time.UnixMilli(reportMS).In(hex.ProtocolTimeZone).Format("060102150405"),
		"shifted instant must still render the original GMT+8 wall clock")

	var occurredOne, receivedOne, occurredZero, receivedZero int64
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT occurred_at_ms, received_at_ms FROM alarms WHERE phone = '188765000001' AND message_serial = 1`).
		Scan(&occurredOne, &receivedOne))
	require.Equal(t, alarmOccurred.UnixMilli()-shiftMS, occurredOne)
	require.Equal(t, alarmReceived.UnixMilli(), receivedOne, "received time must not be shifted")
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT occurred_at_ms, received_at_ms FROM alarms WHERE phone = '188765000001' AND message_serial = 2`).
		Scan(&occurredZero, &receivedZero))
	require.Zero(t, occurredZero, "unknown terminal time must stay zero")
	require.Equal(t, alarmReceived.Add(time.Second).UnixMilli(), receivedZero)

	var mediaLocation, mediaReceivedMS int64
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT location_time, received_at FROM media_files WHERE phone = '188765000001' AND media_id = 9001`).
		Scan(&mediaLocation, &mediaReceivedMS))
	require.Equal(t, mediaTime.UnixMilli()-shiftMS, mediaLocation)
	require.Equal(t, mediaReceived.UnixMilli(), mediaReceivedMS, "received time must not be shifted")

	var (
		resourceCount     int64
		resourceItems     string
		resourceUpdatedAt int64
	)
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT media_count, items_json, updated_at FROM media_resource_lists WHERE phone = '188765000001'`).
		Scan(&resourceCount, &resourceItems, &resourceUpdatedAt))
	require.Equal(t, int64(2), resourceCount, "media_count must not change")
	require.Equal(t, int64(1), resourceUpdatedAt, "server-side updated_at must not be shifted")
	require.NotContains(t, resourceItems, `Z"`, "UTC labels must be converted")
	require.Contains(t, resourceItems, `"startTime":"2026-11-12T10:30:00+08:00"`)
	require.Contains(t, resourceItems, `"endTime":"2026-11-12T10:31:00+08:00"`)

	var items []model.DeviceMedia
	require.NoError(t, json.Unmarshal([]byte(resourceItems), &items))
	require.Len(t, items, 2)
	require.NotNil(t, items[0].StartTime)
	require.Equal(t, time.Date(2026, 11, 12, 10, 30, 0, 0, hex.ProtocolTimeZone).UnixMilli(),
		items[0].StartTime.UnixMilli())
	require.NotNil(t, items[0].EndTime)
	require.Equal(t, time.Date(2026, 11, 12, 10, 31, 0, 0, hex.ProtocolTimeZone).UnixMilli(),
		items[0].EndTime.UnixMilli())
	require.Equal(t, uint8(1), items[0].LogicChannelID, "non-time fields must not change")
	require.Equal(t, uint32(123456), items[0].Size, "non-time fields must not change")
	require.Nil(t, items[1].StartTime, "null time must stay null")
	require.Nil(t, items[1].EndTime, "null time must stay null")
	require.Equal(t, uint8(2), items[1].LogicChannelID)
	require.Equal(t, uint32(789), items[1].Size)

	var storedPayload string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT payload_json FROM media_upload_tasks WHERE phone = '188765000001' AND serial = 7`).
		Scan(&storedPayload))
	require.Equal(t, taskPayload, storedPayload, "platform-issued task must not be rewritten")
}
