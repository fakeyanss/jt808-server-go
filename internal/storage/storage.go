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

// Config 描述持久化后端的选择与连接参数。
type Config struct {
	Type       string // memory | sqlite | mysql
	SQLitePath string // type=sqlite 时必填
	MySQLDSN   string // type=mysql 时必填
}

var (
	defaultMu     sync.RWMutex
	defaultStores *Stores
)

// Stores 聚合持久化仓储，作为业务组件的存储依赖入口。
type Stores struct {
	Devices  repository.DeviceRepository
	Geo      repository.GeoRepository
	Params   repository.DeviceParamsRepository
	Alarms   repository.AlarmRepository
	Regions  repository.RegionRepository
	Industry repository.IndustryRepository

	// Backend 记录实际启用的后端，供启动日志与运维排查使用。
	Backend string
	close   func() error
}

// NewMemory 创建一组互相独立的内存仓储，供测试与新代码注入。
func NewMemory() *Stores {
	return newStores(BackendMemory)
}

func newStores(backend string) *Stores {
	return &Stores{
		Devices:  repository.NewDeviceRepository(),
		Geo:      repository.NewGeoRepository(),
		Params:   repository.NewDeviceParamsRepository(),
		Alarms:   repository.NewAlarmRepository(),
		Regions:  repository.NewRegionRepository(),
		Industry: repository.NewIndustryRepository(),
		Backend:  backend,
	}
}

// Open 按配置装配存储后端。
// 本期 SQL 后端只覆盖 Device 与 DeviceParams，其余仓储仍使用内存实现，
// 因此除设备/参数外的数据在进程重启后不保留（与设计文档 M2 范围一致）。
func Open(cfg Config) (*Stores, error) {
	backend := strings.ToLower(strings.TrimSpace(cfg.Type))
	if backend == "" {
		backend = BackendMemory
	}
	switch backend {
	case BackendMemory:
		return newStores(BackendMemory), nil
	case BackendSQLite, BackendMySQL:
		sqlStore, err := sqlstore.Open(sqlstore.Config{
			Backend: sqlstore.Backend(backend),
			Path:    cfg.SQLitePath,
			DSN:     cfg.MySQLDSN,
		})
		if err != nil {
			return nil, err
		}
		stores := newStores(backend)
		stores.Devices = sqlStore.DeviceRepository()
		stores.Params = sqlStore.DeviceParamsRepository()
		stores.close = sqlStore.Close
		return stores, nil
	default:
		return nil, fmt.Errorf("unsupported storage backend %q, expected memory|sqlite|mysql", cfg.Type)
	}
}

// Init 按配置初始化进程默认存储，并返回实际后端。
// 必须在任何 storage.Default() 调用之前执行；非法配置返回错误，由调用方终止启动。
func Init(cfg Config) (string, error) {
	stores, err := Open(cfg)
	if err != nil {
		return "", err
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultStores = stores
	return stores.Backend, nil
}

// Default 返回进程默认存储实例；未调用 Init 时懒加载内存后端。
// 保留为全局入口的理由：msg_processor 的 processFunc 注册表是包级函数，
// 签名固定为 (ctx, data) error，无法逐条注入依赖；收口为单一 Default() 后，
// 存储来源可追溯。其余组件通过构造函数注入 Stores。
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

// Close 关闭默认存储持有的资源（当前仅 SQL 后端需要）。
func Close() error {
	defaultMu.RLock()
	stores := defaultStores
	defaultMu.RUnlock()
	return stores.Close()
}
