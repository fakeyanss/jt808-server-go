package sqlcore

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSplitStatements(t *testing.T) {
	script := `
-- 注释中的分号不切分;
CREATE TABLE a (id INTEGER);
INSERT INTO a VALUES (1);
INSERT INTO a VALUES ('x;y');
INSERT INTO a VALUES ('it''s; ok');
`
	stmts := SplitStatements(script)
	require.Len(t, stmts, 4)
	require.Equal(t, "CREATE TABLE a (id INTEGER)", stmts[0])
	require.Equal(t, "INSERT INTO a VALUES (1)", stmts[1])
	require.Equal(t, "INSERT INTO a VALUES ('x;y')", stmts[2])
	require.Equal(t, "INSERT INTO a VALUES ('it''s; ok')", stmts[3])
}

// 语义化版本必须按三段数值比较，字符串序会把 1.10.0 排在 1.9.0 前面。
func TestVersionCompareIsNumericPerSegment(t *testing.T) {
	v190 := mustVersion(t, "1.9.0")
	v1100 := mustVersion(t, "1.10.0")
	require.Equal(t, -1, v190.Compare(v1100))
	require.Equal(t, 1, v1100.Compare(v190))
	require.Equal(t, 0, mustVersion(t, "1.0.0").Compare(mustVersion(t, "v1.0.0")))
	require.Equal(t, -1, mustVersion(t, "1.0.0").Compare(mustVersion(t, "1.0.1")))
	require.Equal(t, 1, mustVersion(t, "2.0.0").Compare(mustVersion(t, "1.99.99")))
}

func TestParseMigrationName(t *testing.T) {
	version, err := ParseMigrationName("v1.0.0.sql")
	require.NoError(t, err)
	require.Equal(t, "1.0.0", version.String())

	_, err = ParseMigrationName("20261118_init.sql")
	require.Error(t, err)

	_, err = ParseMigrationName("1.0.sql")
	require.Error(t, err)
}

func mustVersion(t *testing.T, value string) Version {
	t.Helper()
	version, err := ParseVersion(value)
	require.NoError(t, err)
	return version
}
