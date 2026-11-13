package mysql

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// MySQL 迁移目录的版本序列独立断言，不使用 SQLite 的版本号兜底。
func TestExpectedVersionMatchesEmbeddedFiles(t *testing.T) {
	migrations := Plan().Migrations()
	require.Len(t, migrations, 1)
	require.Equal(t, "v1.0.0.sql", migrations[0].Name)
	require.Equal(t, 1, migrations[0].Version.Major)
	require.Equal(t, 0, migrations[0].Version.Minor)
	require.Equal(t, 0, migrations[0].Version.Patch)
	require.Equal(t, "1.0.0", ExpectedVersion().String())
}

// MySQL 建表语句必须显式声明存储引擎与字符集，避免依赖服务端默认值。
func TestCreateTableStatementsDeclareEngineAndCharset(t *testing.T) {
	for _, migration := range Plan().Migrations() {
		for _, stmt := range migration.Stmts {
			if !strings.HasPrefix(stmt, "CREATE TABLE") {
				continue
			}
			require.Contains(t, stmt, "ENGINE=InnoDB", migration.Name)
			require.Contains(t, stmt, "DEFAULT CHARSET=utf8mb4", migration.Name)
		}
	}
}
