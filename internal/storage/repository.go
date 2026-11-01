package storage

import (
	"context"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/pkg/container"
)

// 本文件定义存储边界的业务契约（M1）。
// 现有 *_cache.go 中的具体类型即为内存实现；业务调用点仍通过既有入口访问默认实例，
// M2 起按文件逐步改为构造函数注入。
// 分类沿用 docs/storage-abstraction-design.md：A 执行现场、B 耐用态、C 派生投影。

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
// Ring 是过渡方法，保留既有的 RingBuffer 写入口；M2 把调用点迁移到 Append 后可移除。
type GeoRepository interface {
	Append(ctx context.Context, geo *model.DeviceGeo) error
	Latest(ctx context.Context, phone string) (*model.DeviceGeo, error)
	Ring(ctx context.Context, phone string) *container.RingBuffer
	Delete(ctx context.Context, phone string) error
}

// ConnectionRegistry 连接现场注册表（A 类，仅进程内存，不持久化）。
type ConnectionRegistry interface {
	Get(ctx context.Context, id string) (*model.Session, error)
	Bind(ctx context.Context, session *model.Session) error
	Unbind(ctx context.Context, id string)
	Count(ctx context.Context) int
}

// FragmentAssembler 终端上行分包重组器（A 类，仅进程内存）。
type FragmentAssembler interface {
	CacheSegment(ctx context.Context, seg *model.Segment, serialNumber uint16) bool
	MissingIndexes(ctx context.Context, seg *model.Segment, serialNumber uint16) []uint16
}

// RetransmitTracker 平台下行分包重传跟踪器（A 类，仅进程内存）。
type RetransmitTracker interface {
	CacheFrame(ctx context.Context, phone string, originalSerial, index, total uint16, frame []byte)
	Frames(ctx context.Context, phone string, originalSerial uint16, packetIDs []uint16) [][]byte
	LinkMedia(ctx context.Context, phone string, mediaID uint32, serial uint16)
	MediaSerial(ctx context.Context, phone string, mediaID uint32) (uint16, bool)
}

// DeviceParamsRepository 终端参数仓储（B 类）。
type DeviceParamsRepository interface {
	Get(ctx context.Context, phone string) (*model.DeviceParams, error)
	Replace(ctx context.Context, params *model.DeviceParams) error
	Delete(ctx context.Context, phone string) error
}

// 编译期确认现有内存实现满足接口。
var (
	_ DeviceRepository       = (*DeviceCache)(nil)
	_ GeoRepository          = (*GeoCache)(nil)
	_ ConnectionRegistry     = (*SessionCache)(nil)
	_ FragmentAssembler      = (*SegmentCache)(nil)
	_ RetransmitTracker      = (*sentSegmentCache)(nil)
	_ DeviceParamsRepository = (*DeviceParamsCache)(nil)
)

// NewDeviceRepository 创建独立的内存设备仓储，便于测试与新代码注入。
func NewDeviceRepository() DeviceRepository { return NewDeviceCache() }

// NewGeoRepository 创建独立的内存定位热读仓储。
func NewGeoRepository() GeoRepository { return NewGeoCache() }

// NewConnectionRegistry 创建独立的连接现场注册表（不启动会话数监控）。
func NewConnectionRegistry() ConnectionRegistry { return newSessionCache() }

// NewFragmentAssembler 创建独立的分包重组器。
func NewFragmentAssembler() FragmentAssembler { return NewSegmentCache() }

// NewRetransmitTracker 创建独立的下行重传跟踪器。
func NewRetransmitTracker() RetransmitTracker { return newSentSegmentCache() }

// NewDeviceParamsRepository 创建独立的内存参数仓储。
func NewDeviceParamsRepository() DeviceParamsRepository { return NewDeviceParamsCache() }
