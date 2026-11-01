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
	require.Len(t, migrations, 4)
	require.Equal(t, migrations[len(migrations)-1].version, ExpectedVersion())
}

// 同一库重复执行迁移只应用一次，版本记录不重复。
func TestApplyIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	applied, err := Apply(ctx, db)
	require.NoError(t, err)
	require.Equal(t, ExpectedVersion(), applied)

	again, err := Apply(ctx, db)
	require.NoError(t, err)
	require.Equal(t, 0, again)

	var rows int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&rows))
	require.Equal(t, ExpectedVersion(), rows)

	// 迁移后业务表与车牌索引均存在。
	for _, table := range []string{"devices", "device_params", "terminal_properties", "device_runtime"} {
		var name string
		err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		require.NoError(t, err, "table %s should exist", table)
	}
	var index string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'idx_devices_plate'`).Scan(&index))
	require.Equal(t, "idx_devices_plate", index)
}

// 设备表已不含运行时列，运行时列集中在 device_runtime。
func TestApplySplitsDeviceRuntimeColumns(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	_, err := Apply(ctx, db)
	require.NoError(t, err)

	for _, col := range []string{"session_id", "trans_proto", "last_com_time_ms", "status"} {
		var count int
		require.NoError(t, db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pragma_table_info('devices') WHERE name = ?`, col).Scan(&count))
		require.Zero(t, count, "devices should not contain %s", col)

		require.NoError(t, db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pragma_table_info('device_runtime') WHERE name = ?`, col).Scan(&count))
		require.Equal(t, 1, count, "device_runtime should contain %s", col)
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

func TestParseVersionRejectsUnnumberedFile(t *testing.T) {
	_, err := parseVersion("init.sql")
	require.Error(t, err)
}
