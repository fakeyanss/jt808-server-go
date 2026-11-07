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
// 内部同时维护 sessionID 与手机号两套索引：前者用于按连接取会话，
// 后者用于按终端手机号定位当前活连接（下发命令、顶替判定、API 展示）。
type ConnectionRegistry interface {
	Get(ctx context.Context, id string) (*model.Session, error)
	GetByPhone(ctx context.Context, phone string) (*model.Session, error)
	Bind(ctx context.Context, session *model.Session) error
	// BindPhone 将手机号绑定到会话。若该手机号已绑定其它会话，返回被顶替的旧会话
	// （可能为 nil）；注册表只负责索引切换，旧连接的关闭与残留清理由调用方在锁外执行。
	BindPhone(ctx context.Context, phone, sessionID string) (*model.Session, error)
	// BindPhoneIfFree 仅在手机号未被其它会话占用时建立绑定，用于注册成功后的首次绑定。
	// 已被占用时不做替换，调用方应等待鉴权通过后再用 BindPhone 顶替。
	BindPhoneIfFree(ctx context.Context, phone, sessionID string) (bool, error)
	Unbind(ctx context.Context, id string)
	Count(ctx context.Context) int
}

// FragmentAssembler 终端上行分包重组器（A 类，仅进程内存）。
type FragmentAssembler interface {
	CacheSegment(ctx context.Context, seg *model.Segment, serialNumber uint16) bool
	MissingIndexes(ctx context.Context, seg *model.Segment, serialNumber uint16) []uint16
	// DropPhone 清理该手机号的全部分包缓存（会话被顶替时调用）。
	DropPhone(ctx context.Context, phone string)
}

// RetransmitTracker 平台下行分包重传跟踪器（A 类，仅进程内存）。
type RetransmitTracker interface {
	CacheFrame(ctx context.Context, phone string, originalSerial, index, total uint16, frame []byte)
	Frames(ctx context.Context, phone string, originalSerial uint16, packetIDs []uint16) [][]byte
	LinkMedia(ctx context.Context, phone string, mediaID uint32, serial uint16)
	MediaSerial(ctx context.Context, phone string, mediaID uint32) (uint16, bool)
	// DropPhone 清理该手机号的下行分片与多媒体重传缓存（会话被顶替时调用）。
	DropPhone(ctx context.Context, phone string)
}

// Stores 聚合运行时状态接口，作为组件构造函数的运行时依赖。
// 它刻意与 storage.Stores（持久化仓储）分离，避免把连接现场混进可持久化的组装。
type Stores struct {
	Conn              ConnectionRegistry
	Fragment          FragmentAssembler
	Retrans           RetransmitTracker
	Heartbeat         HeartbeatRegistry
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
		Heartbeat:         NewHeartbeatRegistry(),
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
			Heartbeat:         NewHeartbeatRegistry(),
			ParamResponses:    NewParamResponseBroker(),
			PropertyResponses: NewPropertyResponseBroker(),
			LocationResponses: NewLocationResponseBroker(),
			RegionResponses:   NewRegionResponseBroker(),
			Tracking:          NewLocationTracker(),
		}
	})
	return defaultStores
}
