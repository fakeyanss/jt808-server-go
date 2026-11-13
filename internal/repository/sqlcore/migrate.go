package sqlcore

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Version 是 MAJOR.MINOR.PATCH 三段数值版本。
//
// 为什么不用字符串比较：`1.10.0` 字典序小于 `1.9.0`，会选出错误的“最新版本”。
// 这里统一解析成三段整数比较，避免依赖 SQL 排序规则与位数假设。
type Version struct {
	Major int
	Minor int
	Patch int
}

var semanticVersionPattern = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)

// ParseVersion 解析 "1.0.0" 或 "v1.0.0"。
func ParseVersion(value string) (Version, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	match := semanticVersionPattern.FindStringSubmatch(value)
	if match == nil {
		return Version{}, fmt.Errorf("invalid semantic version %q, expected MAJOR.MINOR.PATCH", value)
	}
	parts := make([]int, 3)
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(match[i+1])
		if err != nil {
			return Version{}, fmt.Errorf("invalid semantic version %q: %w", value, err)
		}
		parts[i] = n
	}
	return Version{Major: parts[0], Minor: parts[1], Patch: parts[2]}, nil
}

// String 返回不带 v 前缀的版本号，作为落库值。
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compare 返回 -1/0/1，按三段数值依次比较。
func (v Version) Compare(other Version) int {
	if v.Major != other.Major {
		return compareInt(v.Major, other.Major)
	}
	if v.Minor != other.Minor {
		return compareInt(v.Minor, other.Minor)
	}
	return compareInt(v.Patch, other.Patch)
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// LegacyVersionError 表示存量库仍记录已废弃的 8 位数字版本。
//
// 不自动升级：数字版本与语义化版本的对应关系无法可靠推断，
// 直接重放 v1.0.0 可能覆盖已有数据。必须由操作者显式重建 dev 库。
type LegacyVersionError struct {
	Value string
}

// Error 给出可执行的处置建议，而不是只抛一句“版本不匹配”。
func (e *LegacyVersionError) Error() string {
	return fmt.Sprintf(
		"database schema version %q uses the removed 8-digit numeric scheme; this dev database predates semantic versioning v1.0.0. "+
			"Recreate the database (delete the sqlite file or drop all tables) and run `jt808-migrate -c <config>` again; "+
			"automatic upgrade from numeric versions is intentionally unsupported", e.Value)
}

// Migration 是一个版本化的迁移脚本。
type Migration struct {
	Version Version
	Name    string
	Stmts   []string
}

// Plan 是一套后端专属的迁移计划。
//
// SQLite 与 MySQL 各自持有独立目录与版本序列，互不共享文件；
// 两个目录的版本号必须同步推进，但断言与执行完全独立。
type Plan struct {
	migrations []Migration
}

// LoadPlan 从后端迁移目录读取全部 SQL 文件并按语义化版本排序。
// 文件是编译期资产，解析失败说明仓库内容有误，直接 panic 以便构建期尽早暴露。
func LoadPlan(scriptFS fs.FS) *Plan {
	entries, err := fs.ReadDir(scriptFS, ".")
	if err != nil {
		panic(fmt.Sprintf("read embedded migrations: %v", err))
	}
	plan := &Plan{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		content, err := fs.ReadFile(scriptFS, name)
		if err != nil {
			panic(fmt.Sprintf("read migration %s: %v", name, err))
		}
		version, err := ParseMigrationName(name)
		if err != nil {
			panic(err.Error())
		}
		plan.migrations = append(plan.migrations, Migration{
			Version: version,
			Name:    name,
			Stmts:   SplitStatements(string(content)),
		})
	}
	if len(plan.migrations) == 0 {
		panic("no migration files found")
	}
	sort.Slice(plan.migrations, func(i, j int) bool {
		cmp := plan.migrations[i].Version.Compare(plan.migrations[j].Version)
		if cmp != 0 {
			return cmp < 0
		}
		return plan.migrations[i].Name < plan.migrations[j].Name
	})
	for i := 1; i < len(plan.migrations); i++ {
		if plan.migrations[i].Version.Compare(plan.migrations[i-1].Version) == 0 {
			panic(fmt.Sprintf("duplicate migration version: %s and %s",
				plan.migrations[i-1].Name, plan.migrations[i].Name))
		}
	}
	return plan
}

// ParseMigrationName 解析文件名 v<MAJOR>.<MINOR>.<PATCH>.sql。
func ParseMigrationName(name string) (Version, error) {
	if !strings.HasSuffix(name, ".sql") {
		return Version{}, fmt.Errorf("migration file %s must end with .sql", name)
	}
	version, err := ParseVersion(strings.TrimSuffix(name, ".sql"))
	if err != nil {
		return Version{}, fmt.Errorf("migration file %s: %w", name, err)
	}
	return version, nil
}

// Migrations 返回按版本升序排列的迁移列表。
func (p *Plan) Migrations() []Migration {
	if p == nil {
		return nil
	}
	return p.migrations
}

// ExpectedVersion 返回该后端内置的最高迁移版本。
func (p *Plan) ExpectedVersion() Version {
	if p == nil || len(p.migrations) == 0 {
		return Version{}
	}
	return p.migrations[len(p.migrations)-1].Version
}

// Apply 按版本顺序执行尚未应用的迁移，返回本次应用的数量。
// 单个版本内全部语句成功后才写入版本记录；已成功版本不会重复应用。
func (p *Plan) Apply(ctx context.Context, db *sql.DB, stmts Statements) (int, error) {
	exists, err := SchemaTableExists(ctx, db, stmts)
	if err != nil {
		return 0, err
	}
	current := Version{}
	if exists {
		current, err = CurrentVersion(ctx, db)
		if err != nil {
			return 0, err
		}
	}

	applied := 0
	for _, m := range p.migrations {
		if m.Version.Compare(current) <= 0 {
			continue
		}
		for i, stmt := range m.Stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return applied, fmt.Errorf("apply migration %s statement %d: %w", m.Name, i+1, err)
			}
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			m.Version.String(), time.Now().UnixMilli()); err != nil {
			return applied, fmt.Errorf("record migration %s: %w", m.Name, err)
		}
		current = m.Version
		applied++
	}
	return applied, nil
}

// SchemaTableExists 判断 schema_migrations 是否已建立；查询语句由方言提供。
func SchemaTableExists(ctx context.Context, db *sql.DB, stmts Statements) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, stmts.SchemaMigrationsTableExistsQuery()).Scan(&count); err != nil {
		return false, fmt.Errorf("check schema_migrations existence: %w", err)
	}
	return count > 0, nil
}

// CurrentVersion 读取已应用的最高语义化版本；表不存在时返回零值。
// 存在 8 位数字版本时返回 *LegacyVersionError，提示重建 dev 库。
func CurrentVersion(ctx context.Context, db *sql.DB) (Version, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return Version{}, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	highest := Version{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return Version{}, fmt.Errorf("scan schema_migrations.version: %w", err)
		}
		if isLegacyNumericVersion(raw) {
			return Version{}, &LegacyVersionError{Value: raw}
		}
		version, err := ParseVersion(raw)
		if err != nil {
			return Version{}, fmt.Errorf("schema_migrations contains invalid version %q: %w", raw, err)
		}
		if version.Compare(highest) > 0 {
			highest = version
		}
	}
	if err := rows.Err(); err != nil {
		return Version{}, fmt.Errorf("iterate schema_migrations: %w", err)
	}
	return highest, nil
}

var legacyNumericVersion = regexp.MustCompile(`^\d{8}$`)

func isLegacyNumericVersion(value string) bool {
	return legacyNumericVersion.MatchString(strings.TrimSpace(value))
}

// SplitStatements 按分号切分 SQL，忽略行注释与单引号内的分号。
//
// 合并为单文件后这一点尤为关键：注释行里出现分号、字符串字面量里出现分号
// 都不能把语句切断。当前迁移只包含简单 DDL/DML，不处理存储过程或自定义分隔符。
func SplitStatements(script string) []string {
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
