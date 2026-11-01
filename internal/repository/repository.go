package repository

import (
	"context"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// 本文件定义持久化仓储的业务契约。
// 分类沿用 docs/storage-abstraction-design.md：B 耐用态、C 派生投影。
// 具体实现均为非导出的内存实现，业务层只依赖接口；装配入口在 internal/storage。

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
