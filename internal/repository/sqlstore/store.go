// Package sqlstore 提供基于 database/sql 的持久化仓储实现。
//
// SQLite 使用纯 Go 驱动 modernc.org/sqlite（无 cgo），MySQL 使用 go-sql-driver/mysql；
// 两种后端复用同一套 SQL 与建表语句，仅在 upsert 语法上做最小方言差异。
package sqlstore

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/repository/migration"
)

// Backend 标识 SQL 后端类型。
type Backend string

const (
	BackendSQLite Backend = "sqlite"
	BackendMySQL  Backend = "mysql"
)

// Config 描述 SQL 后端连接参数，字段由上层 storage 依据配置装配。
type Config struct {
	Backend Backend
	Path    string // SQLite 数据库文件路径
	DSN     string // MySQL DSN，如 user:pass@tcp(host:port)/db

	// AutoMigrate 只对 SQLite 生效：true 时启动自动补齐版本。
	// MySQL 无论该值如何都只校验版本，避免生产环境的运行时 DDL 被权限拒绝。
	AutoMigrate bool
}

// Store 持有连接池，并作为 SQL 仓储的装配入口。
type Store struct {
	db      *stdsql.DB
	backend Backend
}

// Open 打开数据库并准备可用的连接池。
//
// SQLite 在 AutoMigrate=true 时先补齐迁移再启动；其余情况（以及全部 MySQL）只校验
// schema_migrations 版本与程序内置版本一致，不执行任何 DDL。
func Open(cfg Config) (*Store, error) {
	db, ctx, cancel, err := connect(cfg)
	if err != nil {
		return nil, err
	}
	defer cancel()

	if cfg.AutoMigrate && cfg.Backend == BackendSQLite {
		if _, err := migration.Apply(ctx, db); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("apply %s migrations: %w", cfg.Backend, err)
		}
	} else if err := validateSchemaVersion(ctx, db, cfg.Backend); err != nil {
		_ = db.Close()
		return nil, err
	}

	// 不在启动时批量重置 device_runtime：多节点共享同一库时，任一节点启动都会
	// 覆盖其它节点的状态。单机由优雅停机调用 ResetRuntime；异常重启后残留的
	// online/session 由读路径的 TTL 判定收敛为离线（见 deviceRow.device）。
	return &Store{db: db, backend: cfg.Backend}, nil
}

// Migrate 显式执行迁移并返回本次应用的版本数，不在成功后启动服务。
// MySQL 场景也可以由 DBA 直接按文件名顺序执行 *sql 文件。
func Migrate(cfg Config) (int, error) {
	db, ctx, cancel, err := connect(cfg)
	if err != nil {
		return 0, err
	}
	defer cancel()
	defer func() { _ = db.Close() }()

	applied, err := migration.Apply(ctx, db)
	if err != nil {
		return applied, fmt.Errorf("apply %s migrations: %w", cfg.Backend, err)
	}
	return applied, nil
}

// connect 校验后端参数、打开连接池并确认连通性。
func connect(cfg Config) (*stdsql.DB, context.Context, context.CancelFunc, error) {
	if cfg.Backend != BackendSQLite && cfg.Backend != BackendMySQL {
		return nil, nil, nil, fmt.Errorf("unsupported sql storage backend %q", cfg.Backend)
	}
	driver, dsn, err := buildDSN(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	db, err := stdsql.Open(driver, dsn)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open %s database: %w", cfg.Backend, err)
	}
	tunePool(db, cfg.Backend)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := db.PingContext(ctx); err != nil {
		cancel()
		_ = db.Close()
		return nil, nil, nil, fmt.Errorf("connect to %s database: %w", cfg.Backend, err)
	}
	return db, ctx, cancel, nil
}

// validateSchemaVersion 只读取版本并比对，不执行任何 DDL。
func validateSchemaVersion(ctx context.Context, db *stdsql.DB, backend Backend) error {
	expected := migration.ExpectedVersion()
	current, err := migration.CurrentVersion(ctx, db)
	if err != nil {
		return fmt.Errorf("read %s schema version (migrate the database first): %w", backend, err)
	}
	if current < expected {
		return fmt.Errorf(
			"database schema version %d is behind expected %d; run `jt808-server -c <config> migrate`",
			current, expected)
	}
	if current > expected {
		return fmt.Errorf(
			"database schema version %d is ahead of this program's expected %d; use the matching binary",
			current, expected)
	}
	return nil
}

// ResetRuntime 清空运行时投影（session_id 与 status），供单机优雅停机时调用。
//
// 分布式部署下不能直接调用：共享库会被一个节点的停机重置掉其它节点的在线状态，
// 应由 Redis 运行时缓存的全 TTL + 过期扫描回写替代（设计见 §7.4 / §19.6）。
func (s *Store) ResetRuntime(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE device_runtime SET session_id = '', status = 0`); err != nil {
		return fmt.Errorf("reset %s device runtime: %w", s.backend, err)
	}
	return nil
}

// Close 关闭连接池。
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Backend 返回实际 SQL 后端类型。
func (s *Store) Backend() Backend { return s.backend }

// DeviceRepository 返回设备主数据仓储。
func (s *Store) DeviceRepository() repository.DeviceRepository {
	return &deviceRepository{store: s}
}

// DeviceParamsRepository 返回终端参数仓储。
func (s *Store) DeviceParamsRepository() repository.DeviceParamsRepository {
	return &deviceParamsRepository{store: s}
}

// TerminalPropertiesRepository 返回终端属性档案仓储。
// 返回实例内部保留最新值缓存，保证同进程内查询优先命中内存。
func (s *Store) TerminalPropertiesRepository() repository.TerminalPropertiesRepository {
	return &terminalPropertiesRepository{
		store:  s,
		latest: make(map[string]*model.TerminalProperties),
	}
}

// buildDSN 组装驱动名与 DSN。
// SQLite 通过 _pragma 为每个连接设置 busy_timeout 与 WAL，避免并发写立即返回 SQLITE_BUSY。
func buildDSN(cfg Config) (string, string, error) {
	switch cfg.Backend {
	case BackendSQLite:
		if strings.TrimSpace(cfg.Path) == "" {
			return "", "", fmt.Errorf("sqlite storage requires a path")
		}
		return "sqlite", "file:" + cfg.Path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", nil
	case BackendMySQL:
		if strings.TrimSpace(cfg.DSN) == "" {
			return "", "", fmt.Errorf("mysql storage requires a dsn")
		}
		return "mysql", cfg.DSN, nil
	default:
		return "", "", fmt.Errorf("unsupported sql storage backend %q", cfg.Backend)
	}
}

// tunePool 设置连接池边界。
// SQLite 为单写者模型，限定单连接以避免写冲突；MySQL 使用常规服务端连接池参数。
func tunePool(db *stdsql.DB, backend Backend) {
	switch backend {
	case BackendSQLite:
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	case BackendMySQL:
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(30 * time.Minute)
	}
}
