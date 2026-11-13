package sqlcore

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"time"
)

// connectTimeout 是启动时连通性校验与版本读取的总超时。
const connectTimeout = 10 * time.Second

// OpenDatabase 打开连接池、设置连接池边界并校验 schema 版本。
//
// 启动只校验 schema_migrations 版本与程序内置版本一致，不执行任何 DDL；
// 迁移由独立工具 jt808-migrate 显式执行。
func OpenDatabase(driver, dsn, backend string, tune func(*stdsql.DB), stmts Statements, expectedVersion Version) (*stdsql.DB, error) {
	db, err := stdsql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s database: %w", backend, err)
	}
	if tune != nil {
		tune(db)
	}

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to %s database: %w", backend, err)
	}
	if err := ValidateSchemaVersion(ctx, db, backend, stmts, expectedVersion); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// ValidateSchemaVersion 只读取版本并比对，不执行任何 DDL。
//
// 失败信息区分三种情况：表不存在（从未迁移）、版本落后（执行迁移）、
// 版本超前（二进制与库不匹配），以及由 CurrentVersion 返回的旧数字版本错误。
func ValidateSchemaVersion(ctx context.Context, db *stdsql.DB, backend string, stmts Statements, expected Version) error {
	exists, err := SchemaTableExists(ctx, db, stmts)
	if err != nil {
		return fmt.Errorf("check %s schema version: %w", backend, err)
	}
	if !exists {
		return fmt.Errorf(
			"%s database has no schema_migrations table yet; run `jt808-migrate -c <config>` to apply migration %s",
			backend, expected.String())
	}
	current, err := CurrentVersion(ctx, db)
	if err != nil {
		return fmt.Errorf("check %s schema version: %w", backend, err)
	}
	switch current.Compare(expected) {
	case -1:
		return fmt.Errorf(
			"database schema version %s is behind expected %s; run `jt808-migrate -c <config>`",
			current.String(), expected.String())
	case 1:
		return fmt.Errorf(
			"database schema version %s is ahead of this program's expected %s; use the matching binary",
			current.String(), expected.String())
	default:
		return nil
	}
}
