// Package sqlite 提供 SQLite 后端的连接参数、读写连接池与迁移计划。
//
// 仓储的 Go 逻辑由 sqlcore 提供，本包只负责 SQLite 自己的方言语句与 DDL。
// 使用纯 Go 驱动 modernc.org/sqlite，无 cgo 依赖。
//
// 连接模型：
//   - 写池固定 MaxOpenConns=1，是“写写串行”正确性假设的载体，不暴露配置；
//   - 只读池连接数可配（默认 CPU 数），DSN 使用 mode=ro + query_only 强制只读；
//   - 两个池指向同一个库文件，跨池组合不构成事务，读改写必须整段走写池。
package sqlite

import (
	"context"
	stdsql "database/sql"
	"embed"
	"fmt"
	"io/fs"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/fakeyanss/jt808-server-go/internal/repository/sqlcore"
)

// migrateTimeout 是显式迁移的整体超时，避免异常磁盘状态无限挂起。
const migrateTimeout = 10 * time.Minute

// connectTimeout 是只读池建立连接的超时。
const connectTimeout = 10 * time.Second

// writePoolSize 是写池硬编码的连接数，不是调优参数。
const writePoolSize = 1

//go:embed migrations/*.sql
var migrationFS embed.FS

var plan = sqlcore.LoadPlan(mustSubFS(migrationFS, "migrations"))

// Backend 是已完成版本校验的 SQLite 后端句柄。
type Backend struct {
	*sqlcore.Store
}

// Open 打开 SQLite 读写双池并校验 schema 版本。
//
// 启动只校验版本，不执行任何 DDL；迁移由 jt808-migrate 显式调用 Migrate。
// readConns<=0 时按 CPU 数设置只读池连接数。
func Open(path string, readConns int) (*Backend, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("sqlite database requires a path")
	}
	if readConns < 0 {
		return nil, fmt.Errorf("sqlite read conns must be non-negative, got %d", readConns)
	}
	if readConns == 0 {
		readConns = runtime.NumCPU()
	}

	writeDB, err := sqlcore.OpenDatabase("sqlite", writeDSN(path), "sqlite", tuneWritePool, Statements(), ExpectedVersion())
	if err != nil {
		return nil, err
	}
	// 启动断言：写池必须是 1。DSN/调优代码被改动导致连接数漂移时直接拒绝启动。
	if got := writeDB.Stats().MaxOpenConnections; got != writePoolSize {
		_ = writeDB.Close()
		return nil, fmt.Errorf("sqlite write pool must be limited to %d connection, got %d", writePoolSize, got)
	}

	readDB, err := openReadPool(path, readConns)
	if err != nil {
		_ = writeDB.Close()
		return nil, err
	}
	return &Backend{Store: sqlcore.New(writeDB, readDB, "sqlite", Statements())}, nil
}

// Migrate 显式执行 SQLite 迁移并返回本次应用的版本数，不在成功后启动服务。
func Migrate(path string) (int, error) {
	if strings.TrimSpace(path) == "" {
		return 0, fmt.Errorf("sqlite database requires a path")
	}
	db, err := stdsql.Open("sqlite", writeDSN(path))
	if err != nil {
		return 0, fmt.Errorf("open sqlite database: %w", err)
	}
	defer func() { _ = db.Close() }()
	tuneWritePool(db)
	ctx, cancel := context.WithTimeout(context.Background(), migrateTimeout)
	defer cancel()
	applied, err := plan.Apply(ctx, db, Statements())
	if err != nil {
		return applied, fmt.Errorf("apply sqlite migrations: %w", err)
	}
	return applied, nil
}

// ExpectedVersion 返回 SQLite 迁移目录内置的最高版本。
func ExpectedVersion() sqlcore.Version { return plan.ExpectedVersion() }

// Plan 返回 SQLite 迁移计划，供迁移测试断言版本序列。
func Plan() *sqlcore.Plan { return plan }

// writeDSN 为写池设置连接级 PRAGMA：busy_timeout、WAL 与 BEGIN IMMEDIATE。
//
// _txlock=immediate 让 driver 在每个事务开始时发 BEGIN IMMEDIATE，
// 避免延迟事务在锁升级时才失败；不要改成手工 Exec("BEGIN IMMEDIATE")。
func writeDSN(path string) string {
	return "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate"
}

// readDSN 为只读池设置连接级 PRAGMA：mode=ro 由 SQLite 直接拒绝写，
// query_only 再兜一层，防止误用把只读池变成第二个写者。
func readDSN(path string) string {
	return "file:" + path + "?mode=ro&_pragma=busy_timeout(5000)&_pragma=query_only(1)"
}

// openReadPool 打开真正的只读连接池；WAL 模式下可与写池并发读。
func openReadPool(path string, conns int) (*stdsql.DB, error) {
	db, err := stdsql.Open("sqlite", readDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite read pool: %w", err)
	}
	tuneReadPool(db, conns)
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to sqlite read pool: %w", err)
	}
	return db, nil
}

// tuneWritePool 强制写池单连接，保证写写串行。
func tuneWritePool(db *stdsql.DB) {
	db.SetMaxOpenConns(writePoolSize)
	db.SetMaxIdleConns(writePoolSize)
}

// tuneReadPool 设置只读池连接数；默认值与上限由调用方决定。
func tuneReadPool(db *stdsql.DB, conns int) {
	db.SetMaxOpenConns(conns)
	db.SetMaxIdleConns(conns)
}

// mustSubFS 取出嵌入目录下的迁移子目录；目录缺失说明构建资产有误。
func mustSubFS(embedded embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(embedded, dir)
	if err != nil {
		panic(fmt.Sprintf("resolve embedded migrations dir %q: %v", dir, err))
	}
	return sub
}
