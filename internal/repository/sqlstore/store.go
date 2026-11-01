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

	"github.com/fakeyanss/jt808-server-go/internal/repository"
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
}

// Store 持有连接池，并作为 SQL 仓储的装配入口。
type Store struct {
	db      *stdsql.DB
	backend Backend
}

// Open 打开数据库、校验连通性并执行幂等建表。
func Open(cfg Config) (*Store, error) {
	if cfg.Backend != BackendSQLite && cfg.Backend != BackendMySQL {
		return nil, fmt.Errorf("unsupported sql storage backend %q", cfg.Backend)
	}
	driver, dsn, err := buildDSN(cfg)
	if err != nil {
		return nil, err
	}
	db, err := stdsql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s database: %w", cfg.Backend, err)
	}
	tunePool(db, cfg.Backend)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to %s database: %w", cfg.Backend, err)
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize %s schema: %w", cfg.Backend, err)
	}
	return &Store{db: db, backend: cfg.Backend}, nil
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

// migrate 按版本号顺序幂等执行建表语句，并在 schema_migrations 记录已应用版本。
func migrate(ctx context.Context, db *stdsql.DB) error {
	if _, err := db.ExecContext(ctx, schemaMigrationsDDL); err != nil {
		return err
	}
	var current int
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&current); err != nil {
		return err
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		for _, stmt := range m.stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		if _, err := db.ExecContext(ctx,
			"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
			m.version, time.Now().UnixMilli()); err != nil {
			return err
		}
	}
	return nil
}
