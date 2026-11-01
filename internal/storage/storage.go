// Package storage 负责存储层的装配与初始化，不承载具体仓储实现。
//
// 职责边界：
//   - 聚合持久化仓储接口（repository 包）为 Stores，供组件构造注入；
//   - 后续按配置选择 memory / sqlite / mysql 后端（阶段二）；
//   - 不保存运行时状态，运行时状态由 runtime.Stores 独立组装。
//
// 依赖方向：storage → repository；repository 不依赖 storage。
package storage

import (
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

var (
	defaultStores *Stores
	defaultOnce   sync.Once
)

// Stores 聚合持久化仓储，作为业务组件的存储依赖入口。
type Stores struct {
	Devices  repository.DeviceRepository
	Geo      repository.GeoRepository
	Params   repository.DeviceParamsRepository
	Alarms   repository.AlarmRepository
	Regions  repository.RegionRepository
	Industry repository.IndustryRepository
}

// NewMemory 创建一组互相独立的内存仓储，供测试与新代码注入。
func NewMemory() *Stores {
	return &Stores{
		Devices:  repository.NewDeviceRepository(),
		Geo:      repository.NewGeoRepository(),
		Params:   repository.NewDeviceParamsRepository(),
		Alarms:   repository.NewAlarmRepository(),
		Regions:  repository.NewRegionRepository(),
		Industry: repository.NewIndustryRepository(),
	}
}

// Default 返回进程默认存储实例。
// 保留为全局入口的理由：msg_processor 的 processFunc 注册表是包级函数，
// 签名固定为 (ctx, data) error，无法逐条注入依赖；收口为单一 Default() 后，
// 存储来源可追溯。其余组件通过构造函数注入 Stores。
func Default() *Stores {
	defaultOnce.Do(func() {
		defaultStores = NewMemory()
	})
	return defaultStores
}
