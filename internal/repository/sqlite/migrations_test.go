package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/fakeyanss/jt808-server-go/internal/repository/sqlcore"
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

// SQLite 迁移目录的版本序列独立断言，不使用 MySQL 的版本号兜底。
func TestExpectedVersionMatchesEmbeddedFiles(t *testing.T) {
	migrations := Plan().Migrations()
	require.Len(t, migrations, 1)
	require.Equal(t, "v1.0.0.sql", migrations[0].Name)
	require.Equal(t, 1, migrations[0].Version.Major)
	require.Equal(t, 0, migrations[0].Version.Minor)
	require.Equal(t, 0, migrations[0].Version.Patch)
	require.Equal(t, "1.0.0", ExpectedVersion().String())
}

// 同一库重复执行迁移只应用一次，版本记录不重复。
func TestApplyIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	applied, err := Plan().Apply(ctx, db, Statements())
	require.NoError(t, err)
	require.Equal(t, 1, applied)

	again, err := Plan().Apply(ctx, db, Statements())
	require.NoError(t, err)
	require.Zero(t, again)

	var version string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT version FROM schema_migrations`).Scan(&version))
	require.Equal(t, "1.0.0", version)

	for _, table := range []string{
		"devices", "device_params", "terminal_properties",
		"terminal_catalogs", "region_items", "locations", "alarms",
		"media_resource_lists", "media_upload_tasks", "media_upload_notices",
		"media_files", "media_captures", "media_capture_items", "media_search_results",
		"upgrade_results", "attachment_files", "task_leases", "dedup_keys", "gateway_nodes",
		"media_nodes", "session_directory",
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
	_, err := Plan().Apply(ctx, db, Statements())
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'device_runtime'`).Scan(&count))
	require.Zero(t, count)

	for _, col := range []string{"session_id", "trans_proto", "last_com_time_ms", "status"} {
		require.NoError(t, db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pragma_table_info('devices') WHERE name = ?`, col).Scan(&count))
		require.Zero(t, count, "devices should not contain %s", col)
	}
}

// 旧数字版本库必须给出可执行的处置提示，而不是只报版本不匹配。
func TestCurrentVersionRejectsLegacyNumericVersion(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	_, err := db.ExecContext(ctx,
		`CREATE TABLE schema_migrations (version TEXT NOT NULL PRIMARY KEY, applied_at INTEGER NOT NULL)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES ('20261118', 0)`)
	require.NoError(t, err)

	_, err = sqlcore.CurrentVersion(ctx, db)
	require.Error(t, err)
	var legacy *sqlcore.LegacyVersionError
	require.ErrorAs(t, err, &legacy)
	require.Contains(t, err.Error(), "20261118")
	require.Contains(t, err.Error(), "Recreate the database")
}

// 启动路径同样要把旧数字版本翻译成可执行的处置提示。
func TestOpenRejectsLegacySchemaVersion(t *testing.T) {
	path := t.TempDir() + "/jt808.db"
	db, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE schema_migrations (version TEXT NOT NULL PRIMARY KEY, applied_at INTEGER NOT NULL)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES ('20261118', 0)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = Open(path, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "20261118")
	require.Contains(t, err.Error(), "Recreate the database")
}

// 合并成基线后，原有的 BCD 时间平移语句仍可独立执行并保持原语义。
func TestBaselineShiftsBCDTimesToGMT8(t *testing.T) {
	const shiftMS = int64(8 * 60 * 60 * 1000)
	ctx := context.Background()
	db := newTestDB(t)

	for _, stmt := range []string{
		`CREATE TABLE locations (report_time_ms INTEGER NOT NULL)`,
		`CREATE TABLE alarms (occurred_at_ms INTEGER NOT NULL)`,
		`CREATE TABLE media_files (location_time INTEGER NOT NULL)`,
		`CREATE TABLE media_resource_lists (items_json TEXT NOT NULL)`,
	} {
		_, err := db.ExecContext(ctx, stmt)
		require.NoError(t, err)
	}

	locationTime := time.Date(2026, 10, 28, 20, 45, 0, 0, time.UTC)
	_, err := db.ExecContext(ctx, `INSERT INTO locations (report_time_ms) VALUES (?)`, locationTime.UnixMilli())
	require.NoError(t, err)
	alarmTime := time.Date(2026, 10, 28, 21, 0, 0, 0, time.UTC)
	_, err = db.ExecContext(ctx, `INSERT INTO alarms (occurred_at_ms) VALUES (?)`, alarmTime.UnixMilli())
	require.NoError(t, err)
	mediaTime := time.Date(2026, 11, 12, 10, 30, 0, 0, time.UTC)
	_, err = db.ExecContext(ctx, `INSERT INTO media_files (location_time) VALUES (?)`, mediaTime.UnixMilli())
	require.NoError(t, err)
	const items = `[{"startTime":"2026-11-12T10:30:00Z"}]`
	_, err = db.ExecContext(ctx, `INSERT INTO media_resource_lists (items_json) VALUES (?)`, items)
	require.NoError(t, err)

	var bcdStatements []string
	for _, migration := range Plan().Migrations() {
		for _, stmt := range migration.Stmts {
			if strings.Contains(stmt, "28800000") || strings.Contains(stmt, "REPLACE(items_json") {
				bcdStatements = append(bcdStatements, stmt)
			}
		}
	}
	require.Len(t, bcdStatements, 4, "expected four BCD time-shift statements")
	for _, stmt := range bcdStatements {
		_, err := db.ExecContext(ctx, stmt)
		require.NoError(t, err)
	}

	var reportMS, occurredMS, locationMS int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT report_time_ms FROM locations`).Scan(&reportMS))
	require.Equal(t, locationTime.UnixMilli()-shiftMS, reportMS)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT occurred_at_ms FROM alarms`).Scan(&occurredMS))
	require.Equal(t, alarmTime.UnixMilli()-shiftMS, occurredMS)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT location_time FROM media_files`).Scan(&locationMS))
	require.Equal(t, mediaTime.UnixMilli()-shiftMS, locationMS)

	var shifted string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT items_json FROM media_resource_lists`).Scan(&shifted))
	require.Contains(t, shifted, `"startTime":"2026-11-12T10:30:00+08:00"`)
}
