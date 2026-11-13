// Package sqlcore 提供 SQL 仓储的方言无关实现骨架。
//
// 职责边界：
//   - 承载全部仓储的 Go 控制流、扫描与校验逻辑；
//   - 所有方言相关 SQL（冲突处理、加锁、批量插入前缀）由 Backend 包实现
//     Statements 接口提供，本包不出现任何 `if driver == ...` 分支；
//   - 连接、DSN 与连接池参数由各 Backend 包负责，这里只接收已打开的 *sql.DB。
package sqlcore

import (
	"context"
	stdsql "database/sql"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/clock"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// Store 持有连接池、后端标识与方言语句集，是 SQL 仓储的装配根。
//
// db 是写池：所有 Exec、事务与“先查后改”都必须走这里。
// readDB 是只读池：只用于纯读查询；SQLite 后端会指向独立的只读连接池，
// MySQL 后端与 db 相同。仓储实现不得跨池组合读写。
type Store struct {
	db      *stdsql.DB
	readDB  *stdsql.DB
	backend string
	stmts   Statements
	// clock 是进程内唯一时钟源的注入点：判定时刻由调用方传入，
	// 这里只负责记录用途的 updated_at，避免仓储散落 time.Now()。
	clock clock.Clock
}

// New 用已打开并完成版本校验的连接创建 SQL 仓储装配根。
// readDB 为 nil 时退化为与写池共用同一个连接池。
func New(db, readDB *stdsql.DB, backend string, stmts Statements) *Store {
	if readDB == nil {
		readDB = db
	}
	return &Store{db: db, readDB: readDB, backend: backend, stmts: stmts, clock: clock.System()}
}

// DB 返回写池，供迁移工具、写路径与仓储测试执行探针 SQL。
func (s *Store) DB() *stdsql.DB { return s.db }

// ReadDB 返回只读池，仅供纯读查询使用。
func (s *Store) ReadDB() *stdsql.DB { return s.readDB }

// readTimeout 限制单次只读查询的持续时间。
//
// WAL 下未关闭的 rows 或长事务会阻塞 checkpoint，导致 WAL 持续增长且不报错；
// 只读查询必须有界。调用方已有更短 deadline 时以调用方为准。
const readTimeout = 30 * time.Second

// readContext 为只读查询追加默认超时。
func readContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, readTimeout)
}

// Backend 返回实际 SQL 后端类型。
func (s *Store) Backend() string { return s.backend }

// CurrentTimeMillis 读取数据库当前时间（Unix 毫秒），仅供时钟偏差探针观测。
// 它不参与任何业务判定；判定一律使用调用方从进程时钟取得的时刻。
func (s *Store) CurrentTimeMillis(ctx context.Context) (int64, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	var millis int64
	if err := s.readDB.QueryRowContext(ctx, s.stmts.SelectCurrentTimeMillis()).Scan(&millis); err != nil {
		return 0, err
	}
	return millis, nil
}

// Statements 返回该后端的方言语句集。
func (s *Store) Statements() Statements { return s.stmts }

// Ping 校验连接池当前仍可用，供就绪探针使用。
func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := s.db.PingContext(ctx); err != nil {
		return err
	}
	if s.readDB != nil && s.readDB != s.db {
		return s.readDB.PingContext(ctx)
	}
	return nil
}

// Close 关闭连接池。
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	err := s.db.Close()
	if s.readDB != nil && s.readDB != s.db {
		if readErr := s.readDB.Close(); err == nil {
			err = readErr
		}
	}
	return err
}

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

// IndustryRepository 返回行业上报与下行配置仓储。
// 配置类数据（0x8301/0x8303/0x8401/0x8108/0x8900）启动时从 DB 回灌内存。
func (s *Store) IndustryRepository() (repository.IndustryRepository, error) {
	return newIndustryRepository(s)
}

// RegionRepository 返回区域/路线仓储。
// 启动时从 DB 回灌内存，写入按整族事务覆盖。
func (s *Store) RegionRepository() (repository.RegionRepository, error) {
	return newRegionRepository(s)
}
