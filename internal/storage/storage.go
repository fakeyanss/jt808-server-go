// Package storage 负责存储层的装配与初始化，不承载具体仓储实现。
//
// 职责边界：
//   - 聚合持久化仓储接口（repository 包）为 Stores，供组件构造注入；
//   - 按配置选择 memory / sqlite / mysql 后端并完成初始化；
//   - 不保存运行时状态，运行时状态由 runtime.Stores 独立组装。
//
// 依赖方向：storage → repository / repository/sqlstore；repository 不依赖 storage。
package storage

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/repository/sqlstore"
)

// 支持的存储后端类型；空值与 memory 等价，默认 memory 保证既有行为不变。
const (
	BackendMemory = "memory"
	BackendSQLite = "sqlite"
	BackendMySQL  = "mysql"
)

// Config 描述持久化后端的选择、连接参数与保留策略。
type Config struct {
	Type       string // memory | sqlite | mysql
	SQLitePath string // type=sqlite 时必填
	MySQLDSN   string // type=mysql 时必填

	// 保留策略仅 SQL 后端消费；memory 后端保持既有进程内行为不变。
	LocationRetention repository.LocationRetention
	AlarmRetention    repository.AlarmRetention
}

var (
	defaultMu     sync.RWMutex
	defaultStores *Stores
)

// Stores 聚合持久化仓储，作为业务组件的存储依赖入口。
type Stores struct {
	Devices        repository.DeviceRepository
	Geo            repository.GeoRepository
	Params         repository.DeviceParamsRepository
	Properties     repository.TerminalPropertiesRepository
	Alarms         repository.AlarmRepository
	Regions        repository.RegionRepository
	Industry       repository.IndustryRepository
	Media          repository.MediaMetadataRepository
	MediaFiles     repository.MediaFileRepository
	MediaCaptures  repository.MediaCaptureRepository
	MediaSearches  repository.MediaSearchRepository
	UpgradeResults repository.UpgradeResultRepository
	Commands       repository.CommandRepository
	// Locations 为定位历史仓储；memory 后端保持既有热读环形缓冲，本字段为 nil。
	Locations repository.LocationRepository
	// Retention 为历史数据分批清理能力，仅 SQL 后端提供；memory 后端为 nil。
	Retention repository.RetentionStore

	// Backend 记录实际启用的后端，供启动日志与运维排查使用。
	Backend string
	close   func() error
	// check 校验后端连通性；memory 后端为空操作，nil 表示始终可用。
	check func(ctx context.Context) error
}

// NewMemory 创建一组互相独立的内存仓储，供测试与新代码注入。
func NewMemory() *Stores {
	return newStores(BackendMemory)
}

func newStores(backend string) *Stores {
	// 文件与拍摄应答共用同一实现，才能在同一次查询里判断 mediaId 是否已落盘。
	mediaFiles := repository.NewMediaFileRepository()
	return &Stores{
		Devices:        repository.NewDeviceRepository(),
		Geo:            repository.NewGeoRepository(),
		Params:         repository.NewDeviceParamsRepository(),
		Properties:     repository.NewTerminalPropertiesRepository(),
		Alarms:         repository.NewAlarmRepository(),
		Regions:        repository.NewRegionRepository(),
		Industry:       repository.NewIndustryRepository(),
		Media:          repository.NewMediaMetadataRepository(),
		MediaFiles:     mediaFiles,
		MediaCaptures:  mediaFiles,
		MediaSearches:  repository.NewMediaSearchRepository(),
		UpgradeResults: repository.NewUpgradeResultRepository(),
		Commands:       repository.NewCommandRepository(),
		Backend:        backend,
	}
}

// Open 按配置装配存储后端。
// SQL 后端覆盖 Device、DeviceParams、TerminalProperties、Industry、Regions、
// Alarms、Media、Commands 与 Locations；memory 后端保持既有行为不变。
func Open(cfg Config) (*Stores, error) {
	backend := normalizeBackend(cfg.Type)
	switch backend {
	case BackendMemory:
		return newStores(BackendMemory), nil
	case BackendSQLite, BackendMySQL:
		sqlStore, err := sqlstore.Open(sqlConfig(cfg, backend))
		if err != nil {
			return nil, err
		}
		industry, err := sqlStore.IndustryRepository()
		if err != nil {
			_ = sqlStore.Close()
			return nil, err
		}
		regions, err := sqlStore.RegionRepository()
		if err != nil {
			_ = sqlStore.Close()
			return nil, err
		}
		stores := newStores(backend)
		stores.Devices = sqlStore.DeviceRepository()
		stores.Params = sqlStore.DeviceParamsRepository()
		stores.Properties = sqlStore.TerminalPropertiesRepository()
		stores.Industry = industry
		stores.Media = sqlStore.MediaMetadataRepository()
		stores.MediaFiles = sqlStore.MediaFileRepository()
		stores.MediaCaptures = sqlStore.MediaCaptureRepository()
		stores.MediaSearches = sqlStore.MediaSearchRepository()
		stores.UpgradeResults = sqlStore.UpgradeResultRepository()
		stores.Commands = sqlStore.CommandRepository()
		stores.Regions = regions
		stores.Alarms = sqlStore.AlarmRepository(cfg.AlarmRetention)
		stores.Locations = sqlStore.LocationRepository(cfg.LocationRetention)
		stores.Retention = sqlStore.RetentionStore()
		stores.close = sqlStore.Close
		stores.check = sqlStore.Ping
		return stores, nil
	default:
		return nil, fmt.Errorf("unsupported storage backend %q, expected memory|sqlite|mysql", cfg.Type)
	}
}

// Migrate 按配置显式执行迁移并返回本次应用的版本数，不初始化进程默认存储。
// MySQL 场景也可由 DBA 按文件名顺序直接执行 internal/repository/migration 下的 SQL 文件。
func Migrate(cfg Config) (int, error) {
	backend := normalizeBackend(cfg.Type)
	switch backend {
	case BackendMemory:
		return 0, fmt.Errorf("memory storage has no schema to migrate")
	case BackendSQLite, BackendMySQL:
		return sqlstore.Migrate(sqlConfig(cfg, backend))
	default:
		return 0, fmt.Errorf("unsupported storage backend %q, expected memory|sqlite|mysql", cfg.Type)
	}
}

// sqlConfig 将通用配置转换为 SQL 后端配置。
func sqlConfig(cfg Config, backend string) sqlstore.Config {
	return sqlstore.Config{
		Backend: sqlstore.Backend(backend),
		Path:    cfg.SQLitePath,
		DSN:     cfg.MySQLDSN,
	}
}

// normalizeBackend 归一化后端名称；空值与 memory 等价。
func normalizeBackend(name string) string {
	backend := strings.ToLower(strings.TrimSpace(name))
	if backend == "" {
		return BackendMemory
	}
	return backend
}

// Init 按配置初始化进程默认存储，并返回实际实例供装配层显式下传。
// 必须在任何 storage.Default() 调用之前执行；非法配置返回错误，由调用方终止启动。
func Init(cfg Config) (*Stores, error) {
	stores, err := Open(cfg)
	if err != nil {
		return nil, err
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultStores = stores
	return stores, nil
}

// Default 返回进程默认存储实例；未调用 Init 时懒加载内存后端。
// 业务处理器已改为通过 ProcessorDeps 显式注入，Default 仅作为
// 未注入时的回退入口与装配层之外的兼容取用点。
func Default() *Stores {
	defaultMu.RLock()
	stores := defaultStores
	defaultMu.RUnlock()
	if stores != nil {
		return stores
	}

	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultStores == nil {
		defaultStores = newStores(BackendMemory)
	}
	return defaultStores
}

// Close 释放该 Stores 持有的后端资源（内存后端为空操作）。
func (s *Stores) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// Check 校验后端当前可用；memory 后端始终返回 nil。
func (s *Stores) Check(ctx context.Context) error {
	if s == nil || s.check == nil {
		return nil
	}
	return s.check(ctx)
}

// Close 关闭默认存储持有的资源（当前仅 SQL 后端需要）。
func Close() error {
	defaultMu.RLock()
	stores := defaultStores
	defaultMu.RUnlock()
	return stores.Close()
}
