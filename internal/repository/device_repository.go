package repository

import (
	"context"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

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
