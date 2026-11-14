package repository

import (
	"context"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// 本文件定义持久化仓储的业务契约，是接口的唯一来源。
// 分类沿用 docs/archive/storage-abstraction-design.md：B 耐用态、C 派生投影。
// 内存实现在 repository/memory，SQL 实现在 sqlcore + sqlite/mysql，装配入口在 repository/database。

// DeviceRepository 设备主数据仓储（B 类）。
// 实现必须返回快照，调用方修改返回值不得影响仓储内部状态。
type DeviceRepository interface {
	GetByPhone(ctx context.Context, phone string) (*model.Device, error)
	GetByPlate(ctx context.Context, plate string) (*model.Device, error)
	List(ctx context.Context) ([]*model.Device, error)
	// Upsert 整行写入，仅用于导入、CLI 与测试等明确知道全部字段的场景。
	// 运行时的 0x0100/0x0102 必须走 SaveRegistration / SaveAuthState，避免互相覆盖字段。
	Upsert(ctx context.Context, device *model.Device) error
	// SaveRegistration 只写 0x0100 拥有的档案静态字段（设备 ID、车牌、鉴权码、
	// 协议版本描述与版本号、心跳周期）；设备不存在时插入，运行时字段取默认值。
	SaveRegistration(ctx context.Context, device *model.Device) error
	// SaveAuthState 只写 0x0102 拥有的在线状态与终端信息（IMEI、软件版本、
	// 在线状态、会话 ID、最后通信时间）；设备不存在时返回 ErrDeviceNotFound。
	SaveAuthState(ctx context.Context, device *model.Device) error
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

// TerminalPropertiesRepository 终端属性档案仓储（B 类）。
// 数据只来自 0x0107 查询应答，与 Device 的静态字段独立存储。
type TerminalPropertiesRepository interface {
	Get(ctx context.Context, phone string) (*model.TerminalProperties, error)
	Replace(ctx context.Context, properties *model.TerminalProperties) error
	Delete(ctx context.Context, phone string) error
}
