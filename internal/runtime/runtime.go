// Package runtime 承载绑定进程生命周期的执行现场（设计文档 A 类）。
//
// 这些状态的生命周期与 TCP/UDP 连接、进程本身绑定，语义是「连接在则有效」，
// 而不是「写入后仍然存在」，因此不提供持久化实现，也不与 repository 的耐用
// 仓储共用同一套存储契约。多实例部署下它们不可共享，必须由各节点独立持有。
package runtime

import (
	"context"
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

var (
	defaultStores *Stores
	defaultOnce   sync.Once
)

// ConnectionRegistry 连接现场注册表（A 类，仅进程内存）。
// 保存的是持有活连接的 *model.Session 引用，Bind/Unbind 与连接生命周期同步。
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

// Stores 聚合运行时状态接口，作为组件构造函数的运行时依赖。
// 它刻意与 storage.Stores（持久化仓储）分离，避免把连接现场混进可持久化的组装。
type Stores struct {
	Conn              ConnectionRegistry
	Fragment          FragmentAssembler
	Retrans           RetransmitTracker
	ParamResponses    ParamResponseBroker
	PropertyResponses PropertyResponseBroker
	LocationResponses LocationResponseBroker
	RegionResponses   RegionResponseBroker
	Tracking          LocationTracker
}

// NewStores 创建一组互相独立的运行时状态实例，供测试与新代码注入。
func NewStores() *Stores {
	return &Stores{
		Conn:              NewConnectionRegistry(),
		Fragment:          NewFragmentAssembler(),
		Retrans:           NewRetransmitTracker(),
		ParamResponses:    NewParamResponseBroker(),
		PropertyResponses: NewPropertyResponseBroker(),
		LocationResponses: NewLocationResponseBroker(),
		RegionResponses:   NewRegionResponseBroker(),
		Tracking:          NewLocationTracker(),
	}
}

// Default 返回进程默认运行时状态实例。
// 保留为全局入口的理由：msg_processor 的 processFunc 注册表是包级函数，
// 签名固定为 (ctx, data) error，无法逐条注入依赖；收口为单一 Default() 后，
// 运行时状态的来源可追溯。其余组件通过构造函数注入 Stores。
func Default() *Stores {
	defaultOnce.Do(func() {
		defaultStores = &Stores{
			Conn:              newDefaultConnectionRegistry(),
			Fragment:          NewFragmentAssembler(),
			Retrans:           NewRetransmitTracker(),
			ParamResponses:    NewParamResponseBroker(),
			PropertyResponses: NewPropertyResponseBroker(),
			LocationResponses: NewLocationResponseBroker(),
			RegionResponses:   NewRegionResponseBroker(),
			Tracking:          NewLocationTracker(),
		}
	})
	return defaultStores
}
