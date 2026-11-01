// Package migration 以版本化 SQL 文件分发数据库 DDL，并提供最小迁移执行器。
//
// 约束：
//   - 不引入迁移框架；只使用 database/sql；
//   - schema_migrations 由执行器自身引导建表，是唯一保留在 Go 中的 DDL；
//   - 启动路径只读版本，不执行 DDL；显式迁移由 sqlstore.Migrate 或 DBA 直接执行文件。
//
// MySQL 的 DDL 隐式提交，无法与"插入版本记录"同事务；因此一个版本中途失败时可能留下
// 部分表已建、版本未记录的中间态。所有语句均为幂等形式，重跑可补齐。
package migration

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed *.sql
var embeddedSQL embed.FS

// bootstrapDDL 建立迁移元数据表；必须先于读取版本执行。
const bootstrapDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at BIGINT NOT NULL
)`

type versionedMigration struct {
	version int
	name    string
	stmts   []string
}

var migrations = mustLoadMigrations()

// ExpectedVersion 返回本程序内置的最高迁移版本。
func ExpectedVersion() int {
	if len(migrations) == 0 {
		return 0
	}
	return migrations[len(migrations)-1].version
}

// CurrentVersion 读取已应用的最高版本。schema_migrations 不存在时返回错误，
// 由调用方提示先执行迁移。
func CurrentVersion(ctx context.Context, db *sql.DB) (int, error) {
	var current int
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current)
	if err != nil {
		return 0, fmt.Errorf("read schema_migrations: %w", err)
	}
	return current, nil
}

// Apply 引导 schema_migrations 并按版本顺序执行尚未应用的迁移，返回本次应用的数量。
// 单个版本内全部语句成功后才写入版本记录；已成功版本不会重复应用。
func Apply(ctx context.Context, db *sql.DB) (int, error) {
	if _, err := db.ExecContext(ctx, bootstrapDDL); err != nil {
		return 0, fmt.Errorf("bootstrap schema_migrations: %w", err)
	}
	current, err := CurrentVersion(ctx, db)
	if err != nil {
		return 0, err
	}

	applied := 0
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		for i, stmt := range m.stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return applied, fmt.Errorf("apply migration %04d (%s) statement %d: %w",
					m.version, m.name, i+1, err)
			}
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			m.version, time.Now().UnixMilli()); err != nil {
			return applied, fmt.Errorf("record migration %04d (%s): %w", m.version, m.name, err)
		}
		current = m.version
		applied++
	}
	return applied, nil
}

// mustLoadMigrations 读取内嵌 SQL 文件并按文件名前缀解析版本号。
// 文件是编译期资产，解析失败说明仓库内容有误，直接 panic 以便构建期尽早暴露。
func mustLoadMigrations() []versionedMigration {
	entries, err := fs.ReadDir(embeddedSQL, ".")
	if err != nil {
		panic(fmt.Sprintf("read embedded migrations: %v", err))
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	loaded := make([]versionedMigration, 0, len(names))
	for _, name := range names {
		version, err := parseVersion(name)
		if err != nil {
			panic(err.Error())
		}
		content, err := embeddedSQL.ReadFile(name)
		if err != nil {
			panic(fmt.Sprintf("read migration %s: %v", name, err))
		}
		loaded = append(loaded, versionedMigration{
			version: version,
			name:    name,
			stmts:   splitStatements(string(content)),
		})
	}
	return loaded
}

func parseVersion(name string) (int, error) {
	prefix := name
	if idx := strings.IndexByte(name, '_'); idx >= 0 {
		prefix = name[:idx]
	}
	version, err := strconv.Atoi(prefix)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("migration file %s has no positive version prefix", name)
	}
	return version, nil
}

// splitStatements 按分号切分 SQL，忽略行注释与单引号内的分号。
// 当前迁移只包含简单 DDL/DML，不处理存储过程或自定义分隔符。
func splitStatements(script string) []string {
	var (
		stmts       []string
		buf         strings.Builder
		inQuote     bool
		lineComment bool
	)
	flush := func() {
		stmt := strings.TrimSpace(buf.String())
		if stmt != "" {
			stmts = append(stmts, stmt)
		}
		buf.Reset()
	}
	for i := 0; i < len(script); i++ {
		c := script[i]
		if lineComment {
			if c == '\n' {
				lineComment = false
				buf.WriteByte(c)
			}
			continue
		}
		if !inQuote && c == '-' && i+1 < len(script) && script[i+1] == '-' {
			lineComment = true
			i++
			continue
		}
		if c == '\'' {
			inQuote = !inQuote
		}
		if c == ';' && !inQuote {
			flush()
			continue
		}
		buf.WriteByte(c)
	}
	flush()
	return stmts
}
