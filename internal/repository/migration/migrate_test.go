package migration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
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
	require.Len(t, migrations, 3)
	require.Equal(t, "20261101_init.sql", migrations[0].name)
	require.Equal(t, "20261102_persistence_tables.sql", migrations[1].name)
	require.Equal(t, "20261105_drop_device_runtime.sql", migrations[2].name)
	require.Equal(t, 20261105, ExpectedVersion())
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
	require.Equal(t, 1, applied)
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
