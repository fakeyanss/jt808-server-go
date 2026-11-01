package repository

import (
	"context"
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

var (
	defaultStores     *Stores
	defaultStoresOnce sync.Once
)

// 本文件定义存储边界的业务契约与统一入口。
// 分类沿用 docs/storage-abstraction-design.md：A 执行现场、B 耐用态、C 派生投影。
// 具体实现均为非导出的内存实现，业务层只依赖接口。

// DeviceRepository 设备主数据仓储（B 类）。
// 实现必须返回快照，调用方修改返回值不得影响仓储内部状态。
type DeviceRepository interface {
	GetByPhone(ctx context.Context, phone string) (*model.Device, error)
	GetByPlate(ctx context.Context, plate string) (*model.Device, error)
	List(ctx context.Context) ([]*model.Device, error)
	Upsert(ctx context.Context, device *model.Device) error
	DeleteByPhone(ctx context.Context, phone string) error
	DeleteByPlate(ctx context.Context, plate string) error
}

// GeoRepository 定位热读仓储（C 类，派生最新值）。
type GeoRepository interface {
	Append(ctx context.Context, geo *model.DeviceGeo) error
	Latest(ctx context.Context, phone string) (*model.DeviceGeo, error)
	Delete(ctx context.Context, phone string) error
}

// DeviceParamsRepository 终端参数仓储（B 类）。
type DeviceParamsRepository interface {
	Get(ctx context.Context, phone string) (*model.DeviceParams, error)
	Replace(ctx context.Context, params *model.DeviceParams) error
	Delete(ctx context.Context, phone string) error
}

// Stores 聚合持久化仓储接口，作为组件的存储依赖入口。
// 运行时状态（连接现场、分包重组、下行重传）由 runtime.Stores 单独提供，
// 不挂在这里，避免把「连接在则有效」的状态混入可持久化装配。
type Stores struct {
	Devices DeviceRepository
	Geo     GeoRepository
	Params  DeviceParamsRepository
}

// NewStores 创建一组互相独立的内存仓储，供测试与新代码注入。
// 不传任何参数，返回全新的独立实例，不污染默认入口。
func NewStores() *Stores {
	return &Stores{
		Devices: NewDeviceRepository(),
		Geo:     NewGeoRepository(),
		Params:  NewDeviceParamsRepository(),
	}
}

// 编译期确认内存实现满足接口。
var (
	_ DeviceRepository       = (*memoryDeviceRepository)(nil)
	_ GeoRepository          = (*memoryGeoRepository)(nil)
	_ DeviceParamsRepository = (*memoryDeviceParamsRepository)(nil)
)

// NewDeviceRepository 创建独立的内存设备仓储，便于测试与新代码注入。
func NewDeviceRepository() DeviceRepository { return newMemoryDeviceRepository() }

// NewGeoRepository 创建独立的内存定位热读仓储。
func NewGeoRepository() GeoRepository { return newMemoryGeoRepository() }

// NewDeviceParamsRepository 创建独立的内存参数仓储。
func NewDeviceParamsRepository() DeviceParamsRepository { return newMemoryDeviceParamsRepository() }

// Default 返回进程默认 Stores 实例。
// 保留为全局入口的理由：msg_processor 注册表（initProcessOption）是包级函数，
// 其返回的 processFunc 签名固定为 (ctx, data) error，无法逐条注入依赖；
// 改造注册表为接口方法或函数工厂属于更大的结构变更，当前范围先显式收口为
// 单一 Default()，调用点与默认值来源可追溯，后续再渐进迁移。
// 其余组件（Server、API、test/client）通过构造函数注入 Stores，不访问 Default。
func Default() *Stores {
	defaultStoresOnce.Do(func() {
		defaultStores = &Stores{
			Devices: newMemoryDeviceRepository(),
			Geo:     newMemoryGeoRepository(),
			Params:  newMemoryDeviceParamsRepository(),
		}
	})
	return defaultStores
}
