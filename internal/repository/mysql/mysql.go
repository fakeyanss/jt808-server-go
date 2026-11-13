// Package mysql 提供 MySQL 后端的连接参数、连接池与迁移计划。
//
// 仓储的 Go 逻辑由 sqlcore 提供，本包只负责 MySQL 自己的方言语句与 DDL。
// 使用 go-sql-driver/mysql；MySQL 的 DDL 隐式提交，无法与版本记录同事务，
// 中途失败可能留下部分表已建、版本未记录的中间态，需要用幂等 SQL 与人工核对处理。
package mysql

import (
	"context"
	stdsql "database/sql"
	"embed"
	"fmt"
	"io/fs"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/fakeyanss/jt808-server-go/internal/repository/sqlcore"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var plan = sqlcore.LoadPlan(mustSubFS(migrationFS, "migrations"))

// migrateTimeout 是显式迁移的整体超时，避免异常网络状态无限挂起。
const migrateTimeout = 10 * time.Minute

// Backend 是已完成版本校验的 MySQL 后端句柄。
type Backend struct {
	*sqlcore.Store
}

// Open 打开 MySQL 连接池、设置连接池边界并校验 schema 版本。
//
// 启动只校验版本，不执行任何 DDL；迁移由 jt808-migrate 显式调用 Migrate，
// 或由 DBA 直接按文件名顺序执行本包 migrations 目录下的 SQL。
func Open(dsn string) (*Backend, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("mysql database requires a dsn")
	}
	db, err := sqlcore.OpenDatabase("mysql", dsn, "mysql", tunePool, Statements(), ExpectedVersion())
	if err != nil {
		return nil, err
	}
	return &Backend{Store: sqlcore.New(db, nil, "mysql", Statements())}, nil
}

// Migrate 显式执行 MySQL 迁移并返回本次应用的版本数，不在成功后启动服务。
func Migrate(dsn string) (int, error) {
	if strings.TrimSpace(dsn) == "" {
		return 0, fmt.Errorf("mysql database requires a dsn")
	}
	db, err := stdsql.Open("mysql", dsn)
	if err != nil {
		return 0, fmt.Errorf("open mysql database: %w", err)
	}
	defer func() { _ = db.Close() }()
	tunePool(db)
	ctx, cancel := context.WithTimeout(context.Background(), migrateTimeout)
	defer cancel()
	applied, err := plan.Apply(ctx, db, Statements())
	if err != nil {
		return applied, fmt.Errorf("apply mysql migrations: %w", err)
	}
	return applied, nil
}

// ExpectedVersion 返回 MySQL 迁移目录内置的最高版本。
func ExpectedVersion() sqlcore.Version { return plan.ExpectedVersion() }

// Plan 返回 MySQL 迁移计划，供迁移测试断言版本序列。
func Plan() *sqlcore.Plan { return plan }

// tunePool 设置 MySQL 服务端连接池参数。
func tunePool(db *stdsql.DB) {
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
}

// mustSubFS 取出嵌入目录下的迁移子目录；目录缺失说明构建资产有误。
func mustSubFS(embedded embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(embedded, dir)
	if err != nil {
		panic(fmt.Sprintf("resolve embedded migrations dir %q: %v", dir, err))
	}
	return sub
}
