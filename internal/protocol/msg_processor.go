package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	actionpkg "github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/codec/hash"
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/mediaupload"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/fakeyanss/jt808-server-go/internal/uplink"
)

var (
	ErrMsgIDNotSupportted = errors.New("Msg id is not supportted") // 消息ID无法处理，应忽略
	ErrNotAuthorized      = errors.New("Not authorized")           // server校验鉴权不通过
	ErrActiveClose        = errors.New("Active close")             // client无法继续处理，应主动关闭连接
)

// 处理消息的Handler接口
type MsgProcessor interface {
	Process(ctx context.Context, pkt *model.PacketData) (*model.ProcessData, error)
}

// 消息处理方法调用表, <msgId, action>
type processOptions map[uint16]*action

type action struct {
	genData func() *model.ProcessData                       // 定义生成消息的类型。由于go不支持type作为参数，所以这里直接初始化结构体
	process func(context.Context, *model.ProcessData) error // 处理消息的逻辑。可以设置消息字段、根据消息做相应处理逻辑
}

// ProcessorDeps 聚合消息处理器的显式依赖。
// 字段为零值时由构造函数回退到包级默认单例，保证既有装配路径行为不变。
type ProcessorDeps struct {
	Storage *storage.Stores
	Runtime *runtime.Stores
	// Uplink 为空表示上行可靠性未配置（全部 best_effort）。
	Uplink *uplink.Policy
	// UplinkSink 为空表示使用进程内直连处理（standalone 默认）；
	// 非空时上行业务事件交给出口，协议内核仍在本进程完成解码与应答。
	UplinkSink contracts.UplinkSink
	// GatewayID 是事件信封里的网关标识，仅出口启用时需要。
	GatewayID string
	// MediaUpload 为空表示 0x0801 只保留行业内存快照，不落盘也不写媒体文件仓储。
	MediaUpload *mediaupload.Store
	// AuthProvider 是 cluster 下网关的只读鉴权来源；为空时回退本地档案。
	AuthProvider contracts.AuthProvider
	// RegisterCache 是网关本地的注册缓存，保存 0x0100 计算出的鉴权码。
	RegisterCache *RegisterCache
	// UplinkMaxBodyBytes 限制交给出口的原始消息体；超限仍应答但记账并告警。
	UplinkMaxBodyBytes int64
	// OnOversized 是超限事件的旁路回调，用于指标与日志。
	OnOversized func(msgID uint16, phone string, size int)
	// QueryResponse 把网关收到的查询应答体交给下行等待表。
	QueryResponse func(msgID uint16, header *model.MsgHeader, body []byte)
	// Blob 是管控侧 0x0801 的文件写入契约；为空时回退 mediaUpload。
	Blob contracts.BlobStore
}

// ProcessorOption 配置消息处理器。
type ProcessorOption func(*processorSettings)

type processorSettings struct {
	deps ProcessorDeps
}

// WithProcessorDeps 注入处理器依赖。未注入的字段保持默认单例。
func WithProcessorDeps(deps ProcessorDeps) ProcessorOption {
	return func(s *processorSettings) {
		s.deps = deps
	}
}

func defaultProcessorDeps() ProcessorDeps {
	return ProcessorDeps{Storage: storage.Default(), Runtime: runtime.Default()}
}

// 表驱动，初始化消息处理方法组。
// 注册项按语义分组：会话与设备档案、位置与报警、参数与属性、行业上报、
// 多媒体与附件、区域路线、平台下行与模拟器行为。
// 所有 process 字段均为处理器方法值，存储与运行时依赖通过 mp.deps 显式注入。
func (mp *JT808MsgProcessor) initProcessOption() processOptions {
	options := make(processOptions)
	mp.registerSessionAndDevice(options)
	mp.registerPositionAndAlarm(options)
	mp.registerParamsAndProperties(options)
	mp.registerIndustryReports(options)
	mp.registerMediaAndAttachment(options)
	mp.registerRegionRoutes(options)
	mp.registerDownlinkAndSimulator(options)
	return options
}

// registerSessionAndDevice 注册会话、注册鉴权、通用应答与传输可靠性相关的入口。
func (mp *JT808MsgProcessor) registerSessionAndDevice(options processOptions) {
	options[0x0001] = &action{ // 通用应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0001{}} // 无需回复
		},
		process: mp.processMsg0001,
	}
	options[0x0002] = &action{ // 心跳
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0002{}, Outgoing: &model.Msg8001{}}
		},
		process: mp.processMsg0002,
	}
	options[0x0003] = &action{ // 注销
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0003{}, Outgoing: &model.Msg8001{}}
		},
		process: mp.processMsg0003,
	}
	options[0x0004] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg0004{}, Outgoing: &model.Msg8004{}}
	}}
	options[0x0005] = &action{genData: func() *model.ProcessData { return &model.ProcessData{Incoming: &model.Msg0005{}} }, process: mp.processMsg0005}
	options[0x0100] = &action{ // 注册
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0100{}, Outgoing: &model.Msg8100{}}
		},
		process: mp.processMsg0100,
	}
	options[0x0102] = &action{ // 鉴权
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0102{}, Outgoing: &model.Msg8001{}}
		},
		process: mp.processMsg0102,
	}
	options[0x8001] = &action{ // 通用应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8001{}}
		},
		process: mp.processMsg8001,
	}
	options[0x8003] = &action{ // 补传分包请求
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8003{}, Outgoing: &model.Msg0001{}}
		},
		process: mp.processMsg8003,
	}
	options[0x8004] = &action{genData: func() *model.ProcessData { return &model.ProcessData{Incoming: &model.Msg8004{}} }}
	options[0x8100] = &action{ // 注册应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8100{}, Outgoing: &model.Msg0102{}}
		},
		process: mp.processMsg8100,
	}
}

// registerPositionAndAlarm 注册位置、报警与跟踪相关的上行入口。
func (mp *JT808MsgProcessor) registerPositionAndAlarm(options processOptions) {
	options[0x0200] = &action{ // 位置信息上报
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0200{}, Outgoing: &model.Msg8001{}}
		},
		process: mp.processMsg0200,
	}
	options[0x0201] = &action{genData: func() *model.ProcessData { return &model.ProcessData{Incoming: &model.Msg0201{}} }, process: mp.processMsg0201}
	options[0x0704] = &action{ // 批量定位数据上传
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0704{}, Outgoing: &model.Msg8001{}}
		},
		process: mp.processMsg0704,
	}
	options[0x8201] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8201{}, Outgoing: &model.Msg0201{}}
	}, process: mp.processMsg8201}
	options[0x8202] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8202{}, Outgoing: &model.Msg0001{}}
	}, process: mp.processMsg8202}
	options[0x8203] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8203{}, Outgoing: &model.Msg0001{}}
	}}
	options[0x8204] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8204{}, Outgoing: &model.Msg0001{}}
	}}
}

// registerParamsAndProperties 注册终端参数与终端属性档案相关的入口。
func (mp *JT808MsgProcessor) registerParamsAndProperties(options processOptions) {
	options[0x0104] = &action{ // 查询终端参数应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0104{}} // 无需回复
		},
		process: mp.processMsg0104,
	}
	options[0x0107] = &action{genData: func() *model.ProcessData { return &model.ProcessData{Incoming: &model.Msg0107{}} }, process: mp.processMsg0107}
	options[0x8103] = &action{ // 设置终端参数
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8103{}, Outgoing: &model.Msg0001{}}
		},
		process: mp.processMsg8103,
	}
	options[0x8104] = &action{ // 查询终端参数
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8104{}, Outgoing: &model.Msg0104{}}
		},
		process: mp.processMsg8104,
	}
	options[0x8106] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8106{}, Outgoing: &model.Msg0104{}}
	}, process: mp.processMsg8106}
	options[0x8107] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8107{}, Outgoing: &model.Msg0107{}}
	}, process: mp.processMsg8107}
}

// registerIndustryReports 注册行业上报与行业目录相关的入口。
func (mp *JT808MsgProcessor) registerIndustryReports(options processOptions) {
	options[0x0108] = &action{ // 升级结果通知
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0108{}, Outgoing: &model.Msg8001{}}
		},
		process: mp.processIndustryReport,
	}
	options[0x0301] = &action{ // 事件上报
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0301{}, Outgoing: &model.Msg8001{}}
		},
		process: mp.processIndustryReport,
	}
	options[0x0302] = &action{ // 提问应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0302{}, Outgoing: &model.Msg8001{}}
		},
		process: mp.processIndustryReport,
	}
	options[0x0303] = &action{ // 信息点播/取消
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0303{}, Outgoing: &model.Msg8001{}}
		},
		process: mp.processIndustryReport,
	}
	options[0x0500] = &action{ // 车辆控制应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0500{}, Outgoing: &model.Msg8001{}}
		},
		process: mp.processIndustryReport,
	}
	options[0x0700] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg0700{}, Outgoing: &model.Msg8001{}}
	}, process: mp.processIndustryReport}
	options[0x0701] = &action{ // 电子运单上报
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0701{}, Outgoing: &model.Msg8001{}}
		},
		process: mp.processIndustryReport,
	}
	options[0x0702] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg0702{}, Outgoing: &model.Msg8001{}}
	}, process: mp.processIndustryReport}
	options[0x0705] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg0705{}, Outgoing: &model.Msg8001{}}
	}, process: mp.processIndustryReport}
	options[0x0800] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg0800{}, Outgoing: &model.Msg8001{}}
	}, process: mp.processIndustryReport}
	options[0x0801] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg0801{}, Outgoing: &model.Msg8800{}}
	}, process: mp.processMsg0801}
	options[0x0802] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg0802{}, Outgoing: &model.Msg8001{}}
	}, process: mp.processMsg0802}
	options[0x0805] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg0805{}, Outgoing: &model.Msg8001{}}
	}, process: mp.processMsg0805}
	options[0x0900] = &action{ // 数据上行透传
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0900{}, Outgoing: &model.Msg8001{}}
		},
		process: mp.processIndustryReport,
	}
	options[0x0901] = &action{ // 数据压缩上报
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0901{}, Outgoing: &model.Msg8001{}}
		},
		process: mp.processIndustryReport,
	}
	options[0x8108] = &action{ // 终端升级
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8108{}, Outgoing: &model.Msg0001{}}
		},
		process: mp.processIndustryReport,
	}
	options[0x8400] = &action{ // 电话回拨
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8400{}, Outgoing: &model.Msg0001{}}
		},
		process: mp.processIndustryReport,
	}
	options[0x8900] = &action{ // 数据下行透传
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8900{}, Outgoing: &model.Msg0001{}}
		},
		process: mp.processIndustryReport,
	}
}

// registerMediaAndAttachment 注册多媒体元数据、音视频信令与苏标附件相关的入口。
func (mp *JT808MsgProcessor) registerMediaAndAttachment(options processOptions) {
	options[0x1205] = &action{ // 终端上传音视频资源列表
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg1205{}}
		},
		process: mp.processMediaResourceList,
	}
	options[0x1206] = &action{ // 文件上传完成通知，终端上行，无需应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg1206{}}
		},
		process: mp.processMediaSignaling,
	}
	// 苏标附件信令实际由独立附件服务器端口处理；主链路注册解码与通用应答，
	// 避免终端误发到 8080 时被判为不支持消息。0x1212 的 0x9212 应答由附件服务器负责。
	options[0x1210] = &action{
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg1210{}, Outgoing: &model.Msg0001{}}
		},
		process: mp.processAttachmentSignaling,
	}
	options[0x1211] = &action{
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg1211{}, Outgoing: &model.Msg0001{}}
		},
		process: mp.processAttachmentSignaling,
	}
	options[0x1212] = &action{
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg1212{}}
		},
		process: mp.processAttachmentSignaling,
	}
	options[0x8800] = &action{genData: func() *model.ProcessData { return &model.ProcessData{Incoming: &model.Msg8800{}} }, process: mp.processMsg8800}
	options[0x8801] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8801{}, Outgoing: &model.Msg0805{}}
	}}
	options[0x8802] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8802{}, Outgoing: &model.Msg0802{}}
	}, process: mp.processMediaSearchResponse}
	options[0x8803] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8803{}, Outgoing: &model.Msg0001{}}
	}, process: mp.processMsg8803}
	options[0x8804] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8804{}, Outgoing: &model.Msg0001{}}
	}, process: mp.processMsg8804}
	options[0x8805] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8805{}, Outgoing: &model.Msg0001{}}
	}, process: mp.processMsg8805}
	options[0x9101] = &action{ // 实时音视频传输请求，接收方（终端）按协议回通用应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg9101{}, Outgoing: &model.Msg0001{}}
		},
	}
	options[0x9102] = &action{ // 音视频实时传输控制
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg9102{}, Outgoing: &model.Msg0001{}}
		},
	}
	options[0x9105] = &action{ // 实时音视频传输状态通知，终端上行，无需应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg9105{}}
		},
		process: mp.processMediaSignaling,
	}
	options[0x9201] = &action{ // 平台下发远程录像回放请求
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg9201{}, Outgoing: &model.Msg0001{}}
		},
	}
	options[0x9202] = &action{ // 平台下发远程录像回放控制
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg9202{}, Outgoing: &model.Msg0001{}}
		},
	}
	options[0x9205] = &action{ // 查询终端音视频资源列表
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg9205{}, Outgoing: &model.Msg1205{}}
		},
		process: mp.processMsg9205,
	}
	options[0x9206] = &action{ // 文件上传指令
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg9206{}, Outgoing: &model.Msg0001{}}
		},
		process: mp.processMediaUploadTask,
	}
}

// registerRegionRoutes 注册区域路线设置、删除与查询相关的入口。
func (mp *JT808MsgProcessor) registerRegionRoutes(options processOptions) {
	for _, messageID := range []uint16{0x8600, 0x8602, 0x8604, 0x8606} {
		options[messageID] = &action{genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.MsgRegionRouteSet{}, Outgoing: &model.Msg0001{}}
		}, process: mp.processRegionRouteSet}
	}
	options[0x8608] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8608{}, Outgoing: &model.Msg0608{}}
	}, process: mp.processRegionRouteQuery}
	options[0x0608] = &action{genData: func() *model.ProcessData { return &model.ProcessData{Incoming: &model.Msg0608{}} }, process: mp.processRegionResponse}
	for _, messageID := range []uint16{0x8601, 0x8603, 0x8605, 0x8607} {
		options[messageID] = &action{genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg860Delete{}, Outgoing: &model.Msg0001{}}
		}, process: mp.processRegionRouteDelete}
	}
}

// registerDownlinkAndSimulator 注册平台下行命令与终端模拟器专属行为。
func (mp *JT808MsgProcessor) registerDownlinkAndSimulator(options processOptions) {
	options[0x8105] = &action{ // 终端控制，由终端模拟器自动确认
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8105{}, Outgoing: &model.Msg0001{}}
		},
	}
	options[0x8300] = &action{ // 文本信息下发
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8300{}, Outgoing: &model.Msg0001{}}
		},
	}
	options[0x8301] = &action{ // 事件设置
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8301{}, Outgoing: &model.Msg0001{}}
		},
		process: mp.processMsg8301,
	}
	options[0x8302] = &action{ // 提问下发
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8302{}, Outgoing: &model.Msg0001{}}
		},
		process: mp.processMsg8302,
	}
	options[0x8303] = &action{ // 信息点播菜单设置
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8303{}, Outgoing: &model.Msg0001{}}
		},
		process: mp.processMsg8303,
	}
	options[0x8304] = &action{ // 信息服务
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8304{}, Outgoing: &model.Msg0001{}}
		},
		process: mp.processMsg8304,
	}
	options[0x8401] = &action{ // 设置电话本
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8401{}, Outgoing: &model.Msg0001{}}
		},
		process: mp.processMsg8401,
	}
	options[0x8500] = &action{ // 车辆控制
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8500{}, Outgoing: &model.Msg0500{}}
		},
		process: mp.processMsg8500,
	}
	options[0x8700] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.MsgRecordCommand{}, Outgoing: &model.Msg0700{}}
	}, process: mp.processMsg8700}
	options[0x8701] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.MsgRecordCommand{}, Outgoing: &model.Msg0001{}}
	}}
	options[0x8702] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8702{}, Outgoing: &model.Msg0702{}}
	}}
}

// 处理jt808消息的Handler方法
type JT808MsgProcessor struct {
	hooks   actionpkg.Hooks
	deps    ProcessorDeps
	options processOptions
}

// NewJT808MsgProcessor 创建消息处理器。
// hooks 为 nil 时使用 action.Noop，保证不影响协议主流程。
// 未通过 ProcessorOption 注入的依赖回退到包级默认单例。
func NewJT808MsgProcessor(hooks actionpkg.Hooks, opts ...ProcessorOption) *JT808MsgProcessor {
	if hooks == nil {
		hooks = actionpkg.Noop{}
	}
	settings := processorSettings{}
	for _, opt := range opts {
		opt(&settings)
	}
	deps := defaultProcessorDeps()
	if settings.deps.Storage != nil {
		deps.Storage = settings.deps.Storage
	}
	if settings.deps.Runtime != nil {
		deps.Runtime = settings.deps.Runtime
	}
	if settings.deps.Uplink != nil {
		deps.Uplink = settings.deps.Uplink
	}
	if settings.deps.UplinkSink != nil {
		deps.UplinkSink = settings.deps.UplinkSink
	}
	if settings.deps.GatewayID != "" {
		deps.GatewayID = settings.deps.GatewayID
	}
	if settings.deps.MediaUpload != nil {
		deps.MediaUpload = settings.deps.MediaUpload
	}
	mp := &JT808MsgProcessor{
		hooks: hooks,
		deps:  deps,
	}
	mp.options = mp.initProcessOption()
	return mp
}

// ackFirstMsgIDs 列出「应答内容固定、业务结果不参与应答」的上行业务消息。
// 圈定依据是协议语义而非业务类别：应答体在解码阶段即可确定，业务处理只产生
// 落库或运行时副作用。绝大多数应答为 0x8001；0x0801 的应答固定为 0x8800，
// 0x1210/0x1211 为苏标扩展固定 0x0001。0x9105 等无需应答的终端上行不在其中。
// 查询类与业务结果相关的消息必须保持业务先行：
// 0x0100→0x8100、0x0102→0x8001、0x0608/0x0702 等各类查询应答、0x0004 时间查询。
var ackFirstMsgIDs = map[uint16]struct{}{
	0x0002: {}, // 心跳
	0x0003: {}, // 注销
	0x0108: {}, // 升级结果通知
	0x0200: {}, // 位置信息汇报
	0x0301: {}, // 事件上报
	0x0302: {}, // 提问应答
	0x0303: {}, // 信息点播/取消
	0x0500: {}, // 车辆控制应答
	0x0700: {}, // 定位数据批量上传（记录仪）
	0x0701: {}, // 电子运单上报
	0x0702: {}, // 驾驶员身份信息上报
	0x0704: {}, // 批量定位数据上传
	0x0705: {}, // CAN 总线数据上传
	0x0800: {}, // 多媒体事件信息上传
	0x0801: {}, // 多媒体数据上传
	0x0802: {}, // 多媒体数据上传应答
	0x0805: {}, // 摄像头立即拍摄命令应答
	0x0900: {}, // 数据上行透传
	0x0901: {}, // 数据压缩上报
	0x1210: {}, // 苏标附件信息
	0x1211: {}, // 苏标附件信息
}

// AckFirst 报告该消息是否允许先发应答再执行业务。
func (mp *JT808MsgProcessor) AckFirst(msgID uint16) bool {
	_, ok := ackFirstMsgIDs[msgID]
	return ok
}

// Decode 完成查表、分包处理、消息体解码与应答构造，不执行业务逻辑。
// 分包未完成时返回仅含 Outgoing 的 ProcessData，调用方不应再执行业务。
func (mp *JT808MsgProcessor) Decode(ctx context.Context, pkt *model.PacketData) (*model.ProcessData, error) {
	msgID := pkt.Header.MsgID
	if session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session); ok && session != nil {
		log.Debug().Str("session", session.ID).Uint16("msg_id", msgID).Msg("Processing JT808 message")
	}
	if _, ok := mp.options[msgID]; !ok {
		return nil, ErrMsgIDNotSupportted
	}
	if pkt.SegCompleted {
		if session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session); ok && session != nil {
			if pkt.Header.MsgID == 0x0801 {
				cancelMediaRetransmission(session, pkt.Header.PhoneNumber, pkt.Header.MsgID, pkt.Header.SerialNumber)
			} else {
				cancelSegmentRetransmission(session, pkt.Header.PhoneNumber, pkt.Header.MsgID, pkt.Header.SerialNumber)
			}
		}
	}

	// process segment packet
	if pkt.Header.IsFragmented() && !pkt.SegCompleted {
		return processSegmentPacket(ctx, pkt)
	}

	act := mp.options[msgID]
	genDataFn := act.genData
	if genDataFn == nil {
		return nil, ErrMsgIDNotSupportted
	}
	data := genDataFn()

	in := data.Incoming
	err := decodeMessageSafely(in, pkt)
	if err != nil {
		return nil, errors.Wrap(err, "Fail to decode packet to jtmsg")
	}
	if session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session); ok && session != nil {
		acknowledgeResponse(session, in)
	}

	if log.Logger.GetLevel() == zerolog.DebugLevel {
		// print log of msg content
		sessionID := ""
		if session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session); ok && session != nil {
			sessionID = session.ID
		}
		inJSON, err := json.Marshal(redactIncomingForLog(in))
		if err != nil {
			return nil, errors.Wrap(err, "Fail to serialize incoming msg to json")
		}
		// for debug
		log.Debug().Str("id", sessionID).Str("RawMsgID", fmt.Sprintf("0x%04x", in.GetHeader().MsgID)).RawJSON("incoming", inJSON).Msg("Received jt808 msg.")
	}

	// 生成待回复的消息
	if data.Outgoing != nil {
		out := data.Outgoing
		err = out.GenOutgoing(in)
		if err != nil {
			return data, errors.Wrap(err, "Fail to generate outgoing msg")
		}

		// print log of outgoing content
		defer func() {
			if out == nil || log.Logger.GetLevel() != zerolog.DebugLevel {
				return
			}

			outJSON, _ := json.Marshal(out)
			sessionID := ""
			if session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session); ok && session != nil {
				sessionID = session.ID
			}
			// for debug
			log.Debug().Str("id", sessionID).Str("RawMsgID", fmt.Sprintf("0x%04x", out.GetHeader().MsgID)).RawJSON("outgoing", outJSON).
				Msg("Generating jt808 outgoing msg.")
		}()
	}
	return data, nil
}

// ApplyBusiness 执行消息对应的业务处理，并分发生命周期钩子。
// msgID 必须由调用方在 Decode 之前捕获：部分 Outgoing.GenOutgoing 会复用
// incoming 的 Header 指针并改写 MsgID，解码后不能再依赖 Header 查表。
// 钩子基于已解码数据，业务失败时仍会触发（旁路观察语义）。
func (mp *JT808MsgProcessor) ApplyBusiness(ctx context.Context, msgID uint16, data *model.ProcessData) error {
	if data == nil || data.Incoming == nil {
		return nil
	}
	var businessErr error
	act, hasAction := mp.options[msgID]
	if mp.deps.UplinkSink != nil && hasAction {
		owner, _ := OwnershipOf(msgID)
		switch owner {
		case OwnerGateway:
			// 会话内的协议处理（查询 broker、补传等）仍在本进程完成。
			if act.process != nil {
				businessErr = act.process(ctx, data)
			}
			if businessErr == nil && mp.deps.QueryResponse != nil && isQueryResponse(msgID) {
				if packet, ok := ctx.Value(model.PacketDecodeCtxKey{}).(*model.PacketData); ok && packet != nil {
					mp.deps.QueryResponse(msgID, data.Incoming.GetHeader(), packet.Body)
				}
			}
		case OwnerBoth:
			if half := mp.protocolHalf(msgID); half != nil {
				businessErr = half(ctx, data)
			}
			if businessErr == nil {
				businessErr = mp.deliverUplink(ctx, msgID, data)
			}
		default:
			businessErr = mp.deliverUplink(ctx, msgID, data)
		}
	} else if hasAction && act.process != nil {
		businessErr = act.process(ctx, data)
	}
	mp.dispatchHooks(ctx, data)
	return businessErr
}

// deliverUplink 把业务事件交给出口，并对超限消息体做可见的例外处理。
func (mp *JT808MsgProcessor) deliverUplink(ctx context.Context, msgID uint16, data *model.ProcessData) error {
	if mp.deps.UplinkMaxBodyBytes > 0 {
		size := 0
		if packet, ok := ctx.Value(model.PacketDecodeCtxKey{}).(*model.PacketData); ok && packet != nil {
			size = len(packet.Body)
		}
		if int64(size) > mp.deps.UplinkMaxBodyBytes {
			phone := ""
			if header := data.Incoming.GetHeader(); header != nil {
				phone = header.PhoneNumber
			}
			// 超限不投递，但仍让网关按协议应答终止终端重传；必须记账与告警，不得静默。
			if mp.deps.OnOversized != nil {
				mp.deps.OnOversized(msgID, phone, size)
			}
			log.Error().
				Str("phone", phone).
				Str("msg_id", fmt.Sprintf("0x%04X", msgID)).
				Int("body_bytes", size).
				Int64("limit_bytes", mp.deps.UplinkMaxBodyBytes).
				Msg("Uplink body exceeds limit; dropped after ack")
			return nil
		}
	}
	reliability := uplinkReliability(mp.deps.Uplink, msgID, data.Incoming)
	event := buildUplinkEvent(ctx, msgID, data, reliability, mp.deps.GatewayID)
	result, err := mp.deps.UplinkSink.Deliver(ctx, event)
	switch {
	case err != nil:
		return err
	case !result.Accepted:
		return fmt.Errorf("uplink sink rejected event %s: %s", event.EventID, result.Reason)
	}
	return nil
}

// protocolHalf 返回“两边都要”消息在网关侧的协议半边。
func (mp *JT808MsgProcessor) protocolHalf(msgID uint16) func(context.Context, *model.ProcessData) error {
	switch msgID {
	case 0x0001:
		// 网关半边继续处理协议层语义（查询 broker、可靠投递配对）；
		// 管控半边通过 deliverUplink 收到归一化后的 TerminalReply。
		return mp.processMsg0001
	case 0x0100:
		return mp.processRegisterProtocol
	case 0x0102:
		return mp.processAuthProtocol
	default:
		return nil
	}
}

// ApplyControlBusiness 是管控侧的业务入口：注册/鉴权只跑业务半边，
// 其余控制面消息复用同一套 handler。
func (mp *JT808MsgProcessor) ApplyControlBusiness(ctx context.Context, msgID uint16, data *model.ProcessData) error {
	switch msgID {
	case 0x0001:
		return mp.processTerminalReplyBusiness(ctx, data)
	case 0x0100:
		return mp.processRegisterBusiness(ctx, data)
	case 0x0102:
		return mp.processAuthBusiness(ctx, data)
	}
	act, ok := mp.options[msgID]
	if !ok || act.process == nil {
		return nil
	}
	return act.process(ctx, data)
}

// isQueryResponse 判断是否为四类查询的终端应答。
func isQueryResponse(msgID uint16) bool {
	switch msgID {
	case 0x0104, 0x0107, 0x0201, 0x0608:
		return true
	default:
		return false
	}
}

// Process 是 Decode 与 ApplyBusiness 的组合入口，保持既有调用方语义：
// 业务错误返回给调用方，由 Pipeline 决定是否中断应答。
func (mp *JT808MsgProcessor) Process(ctx context.Context, pkt *model.PacketData) (*model.ProcessData, error) {
	msgID := pkt.Header.MsgID
	data, err := mp.Decode(ctx, pkt)
	if err != nil {
		return data, err
	}
	if data == nil {
		return nil, nil
	}
	if data.Incoming == nil {
		if data.Outgoing == nil {
			return nil, nil
		}
		return data, nil
	}

	if businessErr := mp.ApplyBusiness(ctx, msgID, data); businessErr != nil {
		return data, errors.Wrap(businessErr, "Fail to process data")
	}

	if data.Outgoing == nil {
		return nil, nil // 此类型msg不需要回复
	}
	return data, nil
}

// dispatchHooks 在协议处理成功后外发生命周期事件。
// 事件发送失败由 hooks 实现自行处理，不会改变 Process 的返回值。
func (mp *JT808MsgProcessor) dispatchHooks(ctx context.Context, data *model.ProcessData) {
	switch in := data.Incoming.(type) {
	case *model.Msg0100:
		event := actionpkg.RegisterEvent{
			Phone:           in.Header.PhoneNumber,
			DeviceID:        in.DeviceID,
			Plate:           in.PlateNumber,
			ProtocolVersion: in.Header.ProtocolVersion,
			VersionDesc:     versionDesc(in.Header.Attr.VersionDesc),
		}
		if out, ok := data.Outgoing.(*model.Msg8100); ok {
			event.Result = uint8(out.Result)
		}
		mp.hooks.OnRegister(ctx, event)
	case *model.Msg0102:
		event := actionpkg.AuthEvent{
			Phone:           in.Header.PhoneNumber,
			IMEI:            in.IMEI,
			SoftwareVersion: in.SoftwareVersion,
		}
		if out, ok := data.Outgoing.(*model.Msg8001); ok {
			event.Success = out.Result == model.ResultSuccess
		}
		mp.hooks.OnAuth(ctx, event)
	case *model.Msg0200:
		mp.notifyLocation(ctx, in)
	case *model.Msg0704:
		for i := range in.Items {
			mp.notifyLocation(ctx, &in.Items[i].Loc)
		}
	}
}

func versionDesc(v model.VersionType) string {
	switch v {
	case model.Version2019:
		return "2019"
	case model.Version2013:
		return "2013"
	default:
		return "2011"
	}
}

func (mp *JT808MsgProcessor) notifyLocation(ctx context.Context, in *model.Msg0200) {
	mp.hooks.OnLocation(ctx, actionpkg.LocationEvent{
		Phone:      in.Header.PhoneNumber,
		Serial:     in.Header.SerialNumber,
		AlarmSign:  in.AlarmSign,
		StatusSign: in.StatusSign,
		Latitude:   in.Latitude,
		Longitude:  in.Longitude,
		Altitude:   in.Altitude,
		Speed:      in.Speed,
		Direction:  in.Direction,
		Time:       in.Time,
	})
	if in.AlarmSign != 0 {
		extItems := in.ParseExtItems()
		mp.hooks.OnAlarm(ctx, actionpkg.AlarmEvent{
			Phone:     in.Header.PhoneNumber,
			Serial:    in.Header.SerialNumber,
			AlarmSign: in.AlarmSign,
			Latitude:  in.Latitude,
			Longitude: in.Longitude,
			Time:      in.Time,
			ExtItems:  extItems,
			ExtRaw:    model.ExtRawBytes(extItems),
			AlarmID:   in.AlarmIDNumberBytes(),
		})
	}
}

func redactIncomingForLog(in model.JT808Msg) model.JT808Msg {
	switch msg := in.(type) {
	case *model.Msg0702:
		copy := *msg
		copy.Name, copy.QualificationCode, copy.IssuingAuthority, copy.IdentityNumber = "[redacted]", "[redacted]", "[redacted]", "[redacted]"
		return &copy
	case *model.Msg0801:
		copy := *msg
		copy.GeoAlarmBody = nil
		copy.FragmentData = nil
		return &copy
	default:
		return in
	}
}

func decodeMessageSafely(msg model.JT808Msg, pkt *model.PacketData) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.Errorf("malformed message body: %v", recovered)
		}
	}()
	return msg.Decode(pkt)
}

func processSegmentPacket(ctx context.Context, pkt *model.PacketData) (*model.ProcessData, error) {
	// JT1078 4.1节定义，对每个分包回复8001
	headerCopy := *pkt.Header
	attrCopy := *pkt.Header.Attr
	headerCopy.Attr = &attrCopy
	headerCopy.MsgID = 0x8001
	headerCopy.Attr.PacketFragmented = 0
	headerCopy.Attr.PacketFragmentedDesc = model.PacketFragmentedFalse
	headerCopy.Frag = nil
	outgoingMsg := &model.Msg8001{
		Header:             &headerCopy,
		AnswerSerialNumber: pkt.Header.SerialNumber,
		AnswerMessageID:    pkt.Header.MsgID,
		Result:             model.ResultSuccess,
	}
	if session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session); ok && session != nil {
		if pkt.Header.MsgID == 0x0801 {
			scheduleMediaRetransmission(session, pkt)
		} else {
			scheduleSegmentRetransmission(session, pkt)
		}
	}
	return &model.ProcessData{Outgoing: outgoingMsg}, nil
}

// 收到心跳，刷新进程内最近通信时间。
// 运行时状态不落库：心跳只更新 runtime.Heartbeat，供保活判定与 API 展示使用。
func (mp *JT808MsgProcessor) processMsg0002(_ context.Context, data *model.ProcessData) error {
	ctx := context.Background()
	phone := data.Incoming.GetHeader().PhoneNumber
	devices := mp.deps.Storage.Devices
	_, err := devices.GetByPhone(ctx, phone)

	// 缓存不存在，说明设备不合法，需要返回错误，让服务层处理关闭
	if errors.Is(err, repository.ErrDeviceNotFound) {
		return errors.Wrapf(err, "Fail to find device cache, phoneNumber=%s", phone)
	}

	mp.deps.Runtime.Heartbeat.Touch(ctx, phone, time.Now())
	return nil
}

// 收到注销，应清除缓存，断开连接。
func (mp *JT808MsgProcessor) processMsg0003(_ context.Context, data *model.ProcessData) error {
	ctx := context.Background()
	devices := mp.deps.Storage.Devices
	device, err := devices.GetByPhone(ctx, data.Incoming.GetHeader().PhoneNumber)
	// 缓存不存在，说明设备不合法，需要返回错误，让服务层处理关闭
	if errors.Is(err, repository.ErrDeviceNotFound) {
		return errors.Wrapf(err, "Fail to find device cache, phoneNumber=%s", data.Incoming.GetHeader().PhoneNumber)
	}
	// 取消定时任务
	timer := NewKeepaliveTimer()
	timer.Cancel(device.Phone)
	// 清楚缓存
	_ = devices.DeleteByPhone(ctx, device.Phone)
	mp.deps.Runtime.Heartbeat.Remove(ctx, device.Phone)
	// 为避免连接TIMEWAIT，应等待对方主动关闭
	return nil
}

// 收到注册，应校验设备ID，如果可注册，则缓存设备信息并返回鉴权码
func (mp *JT808MsgProcessor) processMsg0100(ctx context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0100)

	devices := mp.deps.Storage.Devices
	// 校验注册逻辑
	out := data.Outgoing.(*model.Msg8100)
	// 车辆已被注册
	if _, err := devices.GetByPlate(ctx, in.PlateNumber); err == nil {
		out.Result = model.ResCarAlreadyRegister
		return nil
	}
	// 终端已被注册
	if _, err := devices.GetByPhone(ctx, in.Header.PhoneNumber); err == nil {
		out.Result = model.ResDeviceAlreadyRegister
		return nil
	}

	session := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	device := model.NewDevice(in, session)
	out.AuthCode = genAuthCode(device) // 设置鉴权码

	_ = devices.Upsert(ctx, device)

	// 手机号未被其它活会话占用时立即绑定；已被占用则不在注册阶段踢旧连接，
	// 留到 0x0102 鉴权通过后再顶替，避免伪造注册报文把在线终端踢下线。
	bound, bindErr := mp.deps.Runtime.Conn.BindPhoneIfFree(ctx, device.Phone, session.ID)
	if bindErr != nil {
		log.Warn().Err(bindErr).Str("phone", device.Phone).Str("session", session.ID).Msg("Fail to bind phone on registration")
	} else if !bound {
		log.Info().Str("phone", device.Phone).Str("session", session.ID).Msg("Phone is already bound, deferring takeover until authentication")
	}

	timer := NewKeepaliveTimer()
	timer.Register(device.Phone)
	return nil
}

// 收到鉴权，应校验鉴权token
func (mp *JT808MsgProcessor) processMsg0102(ctx context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0102)

	devices := mp.deps.Storage.Devices
	device, err := devices.GetByPhone(ctx, in.Header.PhoneNumber)
	// 缓存不存在，说明设备不合法，需要返回错误，让服务层处理关闭
	if errors.Is(err, repository.ErrDeviceNotFound) {
		return errors.Wrapf(err, "Fail to find device cache, phoneNumber=%s", in.Header.PhoneNumber)
	}

	out := data.Outgoing.(*model.Msg8001)
	session, _ := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	// 校验鉴权逻辑
	if in.AuthCode != genAuthCode(device) {
		out.Result = model.ResultFail
		// 手机号已被其它活会话占用：本次鉴权只可能来自冒名连接。
		// 保留原连接与设备档案，只拒绝当前连接，避免伪造报文头把在线终端踢下线。
		if session != nil {
			if owner, ownerErr := mp.deps.Runtime.Conn.GetByPhone(ctx, in.Header.PhoneNumber); ownerErr == nil && owner.ID != session.ID {
				log.Warn().Str("phone", in.Header.PhoneNumber).Str("session", session.ID).Str("owner", owner.ID).
					Msg("Reject authentication from superseded session")
				session.MarkCloseAfterResponse()
				return nil
			}
		}
		// 首次注册鉴权失败：维持既有语义，清理未完成注册的设备记录。
		timer := NewKeepaliveTimer()
		timer.Cancel(device.Phone)
		_ = devices.DeleteByPhone(ctx, device.Phone)
		mp.deps.Runtime.Heartbeat.Remove(ctx, device.Phone)
	} else {
		// 鉴权通过。先完成绑定再写设备档案：若本连接已被并发顶替，
		// 绑定会失败，此时不能用它覆盖新会话的设备状态。
		if session != nil {
			if bindErr := mp.bindPhoneSession(ctx, in.Header.PhoneNumber, session); bindErr != nil {
				log.Warn().Err(bindErr).Str("phone", in.Header.PhoneNumber).Str("session", session.ID).
					Msg("Session was superseded during authentication, skip device update")
				return nil
			}
			device.SessionID = session.ID
			device.TransProto = session.GetTransProto()
		}
		device.Status = model.DeviceStatusOnline
		device.LastestComTime = time.Now()
		device.AuthCode = in.AuthCode
		device.IMEI = in.IMEI
		device.SoftwareVersion = in.SoftwareVersion
		_ = devices.Upsert(ctx, device)
	}

	return nil
}

// bindPhoneSession 在鉴权成功后把手机号绑定到当前会话。
// 若该手机号已有其它活会话，顶替它：关闭旧连接、清空旧会话的等待项，
// 并清理该手机号的分包与重传缓存（缓存按手机号组织，无法区分会话，见设计文档）。
func (mp *JT808MsgProcessor) bindPhoneSession(ctx context.Context, phone string, session *model.Session) error {
	stores := mp.deps.Runtime
	superseded, err := stores.Conn.BindPhone(ctx, phone, session.ID)
	if err != nil {
		log.Warn().Err(err).Str("phone", phone).Str("session", session.ID).Msg("Fail to bind phone after authentication")
		return err
	}
	if superseded == nil || superseded.ID == session.ID {
		return nil
	}
	log.Info().Str("phone", phone).Str("old_session", superseded.ID).Str("new_session", session.ID).
		Msg("Superseding stale session for the same phone")
	superseded.ClosePending()
	if superseded.Conn != nil {
		_ = superseded.Conn.Close()
	}
	stores.Conn.Unbind(ctx, superseded.ID)
	stores.Fragment.DropPhone(ctx, phone)
	stores.Retrans.DropPhone(ctx, phone)
	return nil
}

func genAuthCode(d *model.Device) string {
	var splitByte byte = '_'
	codeBuilder := new(strings.Builder)
	codeBuilder.WriteString(d.ID)
	codeBuilder.WriteByte(splitByte)
	codeBuilder.WriteString(d.Plate)
	codeBuilder.WriteByte(splitByte)
	codeBuilder.WriteString(d.Phone)
	return strconv.Itoa(int(hash.FNV32(codeBuilder.String())))
}

// processMsg0104 routes a terminal parameter response to the API request that
// issued the matching 0x8104 query.
func (mp *JT808MsgProcessor) processMsg0104(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0104)
	if in.Parameters != nil {
		_ = mp.deps.Storage.Params.Replace(context.Background(), in.Parameters)
	}
	mp.deps.Runtime.ParamResponses.Deliver(in)
	return nil
}

// processMsg0107 将属性应答落成属性档案、更新内存最新值，再投递给等待的查询方。
// 三者互不阻断：落库失败只告警，不改变协议应答行为。
func (mp *JT808MsgProcessor) processMsg0107(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0107)
	if err := mp.deps.Storage.Properties.Replace(context.Background(), model.NewTerminalProperties(in)); err != nil {
		log.Warn().Err(err).Str("phone", in.Header.PhoneNumber).Msg("Fail to persist terminal properties")
	}
	mp.deps.Runtime.PropertyResponses.Deliver(in)
	return nil
}

func (mp *JT808MsgProcessor) processMsg8201(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8201)
	out := data.Outgoing.(*model.Msg0201)
	geo, err := mp.deps.Storage.Geo.Latest(context.Background(), in.Header.PhoneNumber)
	if err != nil {
		return err
	}
	if geo.Report != nil {
		location := *geo.Report
		locationHeader := *out.Header
		locationHeader.MsgID = 0x0200
		location.Header = &locationHeader
		out.Location = &location
		return nil
	}
	if geo.Geo == nil || geo.Location == nil || geo.Drive == nil {
		return repository.ErrGisNotFound
	}
	out.Location = &model.Msg0200{
		Header:     out.Header,
		StatusSign: geo.Geo.Encode(),
		Latitude:   uint32(geo.Location.Latitude * float64(model.LocationAccuracy)),
		Longitude:  uint32(geo.Location.Longitude * float64(model.LocationAccuracy)),
		Altitude:   geo.Location.Altitude,
		Speed:      uint16(geo.Drive.Speed * float64(model.SpeedAccuracy)),
		Direction:  geo.Drive.Direction,
		Time:       hex.FormatTime(geo.Time),
	}
	return nil
}

func (mp *JT808MsgProcessor) processMsg0201(_ context.Context, data *model.ProcessData) error {
	mp.deps.Runtime.LocationResponses.Deliver(data.Incoming.(*model.Msg0201))
	return nil
}

func (mp *JT808MsgProcessor) processMsg8202(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8202)
	mp.deps.Runtime.Tracking.Set(in.Header.PhoneNumber, in.TimeInterval, in.ValidityPeriod)
	return nil
}

// 收到位置信息汇报，回复通用应答
func (mp *JT808MsgProcessor) processMsg0200(ctx context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0200)

	devices := mp.deps.Storage.Devices
	device, err := devices.GetByPhone(ctx, in.Header.PhoneNumber)
	// 缓存不存在，说明设备不合法，需要返回错误，让服务层处理关闭
	if errors.Is(err, repository.ErrDeviceNotFound) {
		return errors.Wrapf(err, "Fail to find device cache, phoneNumber=%s", in.Header.PhoneNumber)
	}
	if err := mp.deps.Storage.Alarms.Record(ctx, in.Header.PhoneNumber, in.Header.SerialNumber, in.AlarmSign, in.Latitude, in.Longitude, in.Time); err != nil {
		// 报警落库失败需要上抛：at_least_once 分类下不应答，由终端重传。
		return errors.Wrapf(err, "record alarm, phoneNumber=%s", in.Header.PhoneNumber)
	}

	// 解析状态位编码
	dg := &model.DeviceGeo{}
	err = dg.Decode(device.Phone, in)
	if err != nil {
		return errors.Wrapf(err, "Fail to decode device geo, phoneNumber=%s", device.Phone)
	}

	if dg.Geo.ACCStatus == 0 { // ACC关闭，设备休眠
		device.Status = model.DeviceStatusSleeping
		device.LastestComTime = time.Now()
		_ = devices.Upsert(ctx, device)
	}

	_ = mp.deps.Storage.Geo.Append(ctx, dg)
	return mp.recordLocationHistory(ctx, 0x0200, []*model.Msg0200{in})
}

// processMsg0704 保存批量定位快照，并把每个数据项写入定位历史。
// 0x0704 数据项内没有独立消息流水号，无法与报警去重口径对齐，因此只记定位、不记报警。
func (mp *JT808MsgProcessor) processMsg0704(ctx context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0704)
	mp.deps.Storage.Industry.Record(in)

	locs := make([]*model.Msg0200, 0, len(in.Items))
	for i := range in.Items {
		locs = append(locs, &in.Items[i].Loc)
	}
	return mp.recordLocationHistory(ctx, 0x0704, locs)
}

// recordLocationHistory 写入定位历史；memory 后端未启用 Locations 时跳过。
// 返回错误供 at_least_once 分类判定：落库失败时本次不应答，等待终端重传。
func (mp *JT808MsgProcessor) recordLocationHistory(ctx context.Context, msgID uint16, locs []*model.Msg0200) error {
	repo := mp.deps.Storage.Locations
	if repo == nil || len(locs) == 0 {
		return nil
	}
	records := make([]repository.LocationRecord, 0, len(locs))
	event := UplinkEventFromContext(ctx)
	for index, loc := range locs {
		if loc.Header == nil {
			continue
		}
		records = append(records, repository.LocationRecord{
			Phone:      loc.Header.PhoneNumber,
			MsgID:      msgID,
			AlarmSign:  loc.AlarmSign,
			StatusSign: loc.StatusSign,
			Latitude:   loc.Latitude,
			Longitude:  loc.Longitude,
			Altitude:   loc.Altitude,
			Speed:      loc.Speed,
			Direction:  loc.Direction,
			ExtItems:   loc.ExtItems,
			OccurredAt: loc.Time,
			ReceivedAt: time.Now(),
			DedupKey:   locationDedupKey(event.EventID, index),
		})
	}
	if len(records) == 0 {
		return nil
	}
	if err := repo.Record(ctx, records); err != nil {
		return errors.Wrapf(err, "record location history, phoneNumber=%s, msgID=0x%04X", records[0].Phone, msgID)
	}
	return nil
}

// acknowledgeResponse 只清算与该应答精确对应的待确认项：
// 携带应答流水号的应答按流水号唯一匹配；0x0608/0x0702/0x8800 的协议体
// 不含流水号，仅在同类等待项唯一时清算。所有不匹配的应答只记告警，不清除任何等待项。
func acknowledgeResponse(session *model.Session, in model.JT808Msg) {
	if in == nil || in.GetHeader() == nil {
		return
	}
	header := in.GetHeader()
	if serial, ok := model.AnswerSerial(in); ok {
		matched := false
		if reply, isTerminalReply := in.(*model.Msg0001); isTerminalReply && reply != nil {
			// 0x0001 携带终端执行结果，必须透传给 outbox，避免把失败应答当成成功。
			matched = session.AcknowledgeSerialWithResult(serial, header.PhoneNumber, reply.Result)
		} else {
			matched = session.AcknowledgeSerial(serial, header.PhoneNumber)
		}
		if !matched {
			log.Warn().Str("session", session.ID).Uint16("msg_id", header.MsgID).Uint16("answer_serial", serial).
				Msg("Received response without matching pending message")
		}
		return
	}
	switch header.MsgID {
	case 0x0608, 0x0702, 0x8800:
		if !session.AcknowledgeTypedResponse(header.MsgID, header.PhoneNumber) {
			log.Warn().Str("session", session.ID).Uint16("msg_id", header.MsgID).
				Msg("Received response without a unique pending message")
		}
	}
}

func (mp *JT808MsgProcessor) processMsg0001(ctx context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0001)
	if in.AnswerMessageID == 0x8203 {
		if _, err := mp.deps.Storage.Alarms.ConfirmByCommand(ctx, in.Header.PhoneNumber, in.AnswerSerialNumber); err != nil {
			log.Error().Err(err).Str("phone", in.Header.PhoneNumber).Msg("confirm alarm by command")
		}
	}
	return nil
}

func (mp *JT808MsgProcessor) processMsg8001(ctx context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8001)
	if in.AnswerMessageID != 0x0102 {
		return nil
	}
	// 收到8001消息，说明此时是作为终端设备
	if in.Result == model.ResultSuccess {
		// 回复成功，说明之前注册成功，为方便后续处理，将设备状态改为Online并缓存
		devices := mp.deps.Storage.Devices
		device, err := devices.GetByPhone(context.Background(), in.Header.PhoneNumber)
		if errors.Is(err, repository.ErrDeviceNotFound) {
			return nil
		}
		if device.Status != model.DeviceStatusOffline {
			return nil
		}

		// 仅在未注册成功时执行一次
		device.Status = model.DeviceStatusOnline
		device.LastestComTime = time.Now()
		_ = devices.Upsert(context.Background(), device)
	}

	return nil
}

// processMsg8003 is used by the client simulator to replay requested fragment
// frames. A production terminal handles the same downlink command itself.
func (mp *JT808MsgProcessor) processMsg8003(ctx context.Context, data *model.ProcessData) error {
	request := data.Incoming.(*model.Msg8003)
	session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	if !ok || session == nil {
		return nil
	}
	frames := mp.deps.Runtime.Retrans.Frames(context.Background(), request.Header.PhoneNumber, request.OriginalSerialNumber, request.PacketIDs)
	if frames == nil {
		log.Warn().Str("device", request.Header.PhoneNumber).Uint16("original_serial", request.OriginalSerialNumber).Msg("No cached simulator fragments for retransmission request")
		return nil
	}
	for i, frame := range frames {
		if frame == nil {
			log.Warn().Str("device", request.Header.PhoneNumber).Uint16("packet_id", request.PacketIDs[i]).Msg("Requested simulator fragment is not cached")
			continue
		}
		if err := session.WriteFrame(frame); err != nil {
			return errors.Wrap(err, "failed to retransmit simulator fragment")
		}
	}
	return nil
}

// processMsg0005 replays cached platform fragments requested by a terminal.
func (mp *JT808MsgProcessor) processMsg0005(ctx context.Context, data *model.ProcessData) error {
	request := data.Incoming.(*model.Msg0005)
	session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	if !ok || session == nil {
		return nil
	}
	mp.retransmitCachedSegments(session, request.Header.PhoneNumber, request.OriginalSerialNumber, request.PacketIDs)
	return nil
}

// processMsg8800 replays media upload fragments retained by the terminal
// simulator. JT808 transport-level 0x8003 recovery remains independent.
func (mp *JT808MsgProcessor) processMsg8800(ctx context.Context, data *model.ProcessData) error {
	request := data.Incoming.(*model.Msg8800)
	if len(request.RetransmitIDs) == 0 {
		return nil
	}
	session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	if !ok || session == nil {
		return nil
	}
	serial, ok := mp.deps.Runtime.Retrans.MediaSerial(context.Background(), request.Header.PhoneNumber, request.MultiMediaID)
	if !ok {
		log.Warn().Str("device", request.Header.PhoneNumber).Uint32("media_id", request.MultiMediaID).Msg("No cached media fragments for retransmission request")
		return nil
	}
	mp.retransmitCachedSegments(session, request.Header.PhoneNumber, serial, request.RetransmitIDs)
	return nil
}

func (mp *JT808MsgProcessor) retransmitCachedSegments(session *model.Session, phone string, originalSerial uint16, packetIDs []uint16) {
	frames := mp.deps.Runtime.Retrans.Frames(context.Background(), phone, originalSerial, packetIDs)
	if frames == nil {
		log.Warn().Str("device", phone).Uint16("original_serial", originalSerial).Msg("No cached fragments for retransmission request")
		return
	}
	for i, frame := range frames {
		if frame == nil {
			log.Warn().Str("device", phone).Uint16("packet_id", packetIDs[i]).Msg("Requested fragment is not cached")
			continue
		}
		if err := session.WriteFrame(frame); err != nil {
			log.Error().Err(err).Str("device", phone).Msg("Failed to retransmit cached fragment")
			return
		}
	}
}

// 收到注册应答，回复鉴权
func (mp *JT808MsgProcessor) processMsg8100(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8100)
	out := data.Outgoing.(*model.Msg0102)

	device, err := mp.deps.Storage.Devices.GetByPhone(context.Background(), in.Header.PhoneNumber)
	if errors.Is(err, repository.ErrDeviceNotFound) {
		// JT808 2013 registration responses carry the auth code in the response.
		// A simulator that does not keep a local device cache can still continue
		// the authentication exchange; 2019-only identity fields remain empty.
		return nil
	}

	out.IMEI = device.IMEI
	out.SoftwareVersion = device.SoftwareVersion

	return nil
}

// 收到设置终端参数请求，回复通用应答
func (mp *JT808MsgProcessor) processMsg8103(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8103)
	ctx := context.Background()
	paramsRepo := mp.deps.Storage.Params
	params, err := paramsRepo.Get(ctx, in.GetHeader().PhoneNumber)
	if errors.Is(err, repository.ErrDeviceParamsNotFound) {
		params = in.Parameters
	} else {
		params.Update(in.Parameters)
	}
	_ = paramsRepo.Replace(ctx, params)

	return nil
}

// 收到查询终端参数请求，回复终端参数(此时是作为client进程)
func (mp *JT808MsgProcessor) processMsg8104(_ context.Context, data *model.ProcessData) error {
	// todo: generate by config
	out := data.Outgoing.(*model.Msg0104)
	params, err := mp.deps.Storage.Params.Get(context.Background(), out.GetHeader().PhoneNumber)
	if errors.Is(err, repository.ErrDeviceParamsNotFound) {
		out.Parameters = &model.DeviceParams{}
		// 模拟一个固定的参数
		paramCnt := 39
		paramByteStr := "00000001044B687673" +
			"00000002046E764E65" +
			"000000030449716B57" +
			"000000040452704A36" +
			"0000000504524D6D52" +
			"00000006043535774E" +
			"00000007044743525A" +
			"000000101058676767375A3558584B44376E625661" +
			"000000111035625067564C37537A4C616672774E36" +
			"000000121048684E7669576777494D6E493555624D" +
			"000000131064527432655967746479316A46485745" +
			"0000001410676554657949565F7A537362694A6E54" +
			"0000001510556F6868585171533565773575385562" +
			"000000161068336D7550796A6E584D387933587173" +
			"000000171075397A57597452444C4863614E6A5278" +
			"00000018044E586A4F000000190438646C69" +
			"0000001A104E30775635786D4A664D6F6338616650" +
			"0000001B04626C3676" +
			"0000001C0438366537" +
			"0000001D104F344C42704B737445687A4F4C564635" +
			"0000002004386C5255" +
			"00000021045041666E" +
			"000000220457456B39" +
			"00000023106138334C43765737546D5051736E3152" +
			"0000002410427559785F4937584E694F63364E4D49" +
			"0000002510653756707A595579763576596433346C" +
			"0000002610544E376C476643386A4B4D496E4C3967" +
			"00000027045057586E0000002804674C636C" +
			"0000002904474F6A64" +
			"0000002C0445493157" +
			"0000002D04634E507A" +
			"0000002E0453445442" +
			"0000002F04525F647A" +
			"00000030045A4A6830" +
			"00000031026C64" +
			"000000320409302130" +
			"000000760F030000010100010202000103030001"
		if err := out.Parameters.Decode(out.Header.PhoneNumber, uint8(paramCnt), hex.Str2Byte(paramByteStr)); err != nil {
			return err
		}
		_ = mp.deps.Storage.Params.Replace(context.Background(), out.Parameters)
	} else {
		out.Parameters = params
	}

	return nil
}

func (mp *JT808MsgProcessor) processMsg8106(ctx context.Context, data *model.ProcessData) error {
	if err := mp.processMsg8104(ctx, data); err != nil {
		return err
	}
	in := data.Incoming.(*model.Msg8106)
	out := data.Outgoing.(*model.Msg0104)
	filtered := &model.DeviceParams{DevicePhone: out.Parameters.DevicePhone}
	for _, id := range in.ParamIDs {
		for _, p := range out.Parameters.Params {
			if p.ParamID == id {
				filtered.Params = append(filtered.Params, p)
				break
			}
		}
	}
	filtered.ParamCnt = uint8(len(filtered.Params))
	out.Parameters = filtered
	return nil
}

func (mp *JT808MsgProcessor) processMsg8107(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8107)
	id := in.Header.PhoneNumber
	if in.Header.Attr.VersionDesc == model.Version2013 && len(id) > 7 {
		id = id[len(id)-7:]
	}
	terminalModel := "JT808-SIMULATOR"
	if in.Header.Attr.VersionDesc == model.Version2011 {
		terminalModel = "JT808-SI"
	}
	data.Outgoing = &model.Msg0107{Header: in.Header, TerminalType: 1, ManufacturerID: "FAKEY", TerminalModel: terminalModel, TerminalID: id, ICCID: "89860000000000000000", HardwareVersion: "sim-hw-1", FirmwareVersion: "sim-fw-1", GNSSProperties: 1, CommunicationProperties: 1}
	return nil
}

func (mp *JT808MsgProcessor) processMsg9205(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg9205)
	out := data.Outgoing.(*model.Msg1205)
	out.Items = nil
	for _, media := range mp.deps.Storage.Industry.MediaUploads(in.Header.PhoneNumber) {
		if in.LogicChannelID != 0 && media.LogicChannelID != in.LogicChannelID {
			continue
		}
		if !mediaTypeMatches(in.MediaType, media.MultiMediaType) {
			continue
		}
		var location model.Msg0200
		if err := location.Decode(&model.PacketData{Header: in.Header, Body: media.GeoAlarmBody}); err != nil {
			continue
		}
		if !mediaTimeInRange(location.Time, in.StartTime, in.EndTime) {
			continue
		}
		item := model.DeviceMedia{}
		item.LogicChannelID = media.LogicChannelID
		t := hex.ParseTime(location.Time)
		item.StartTime = &t
		item.EndTime = &t
		item.AlarmSign = location.AlarmSign
		item.AlarmSignExt = 0
		item.MediaType = media.MultiMediaType
		item.StreamType = 0
		item.StorageType = 0
		item.Size = uint32(len(media.FragmentData))
		out.Items = append(out.Items, item)
	}
	out.MediaCount = uint32(len(out.Items))
	return nil
}

// mediaTypeMatches reports whether a stored 0x0801 media type satisfies a
// 0x9205 query media type. 0x9205 uses 0=音视频, 1=音频, 2=视频, 3=视频或音视频.
func mediaTypeMatches(queryType, storedType uint8) bool {
	switch queryType {
	case 0:
		return true
	case 1:
		return storedType == 1
	case 2:
		return storedType == 2
	case 3:
		return storedType == 0 || storedType == 2
	default:
		return true
	}
}

// mediaTimeInRange reports whether a BCD time string falls within the optional
// query time range. Zero boundaries mean no constraint.
func mediaTimeInRange(t string, start, end *time.Time) bool {
	if (start == nil && end == nil) || t == "" {
		return true
	}
	loc, err := time.ParseInLocation("060102150405", t, hex.ProtocolTimeZone)
	if err != nil {
		return true
	}
	if start != nil && loc.Before(*start) {
		return false
	}
	if end != nil && loc.After(*end) {
		return false
	}
	return true
}

func (mp *JT808MsgProcessor) processIndustryReport(ctx context.Context, data *model.ProcessData) error {
	mp.deps.Storage.Industry.Record(data.Incoming)
	// 0x0108 升级结果需要跨重启保留，写入独立仓储而不是只留内存快照。
	if result, ok := data.Incoming.(*model.Msg0108); ok && result.Header != nil {
		return mp.deps.Storage.UpgradeResults.Save(ctx, &repository.UpgradeResult{
			Phone:       result.Header.PhoneNumber,
			UpgradeType: result.UpgradeType,
			Result:      result.Result,
			Serial:      result.Header.SerialNumber,
			ReceivedAt:  time.Now(),
		})
	}
	return nil
}

// processMsg0801 把多媒体数据落盘并写入媒体文件仓储。
//
// 到达这里的 0x0801 都是完整消息：分包由 runtime 重组完成后再进入业务，
// 非分包单包即为完整文件，因此这里不需要再判断包序号。
// 行业内存快照保留原有行为，落盘失败时仍可通过 Industry 查到最近一条上报。
func (mp *JT808MsgProcessor) processMsg0801(ctx context.Context, data *model.ProcessData) error {
	upload, ok := data.Incoming.(*model.Msg0801)
	if !ok {
		return nil
	}
	mp.deps.Storage.Industry.Record(upload)
	if mp.deps.MediaUpload == nil {
		log.Warn().Msg("media upload store not configured, skip 0x0801 persistence")
		return nil
	}
	file, err := mp.deps.MediaUpload.Save(upload)
	if err != nil {
		return err
	}
	return mp.deps.Storage.MediaFiles.Save(ctx, file)
}

// processMsg0802 把 0x8802 检索命令的应答（0x0802）写入检索结果仓储。
// 行业内存快照保留原有行为；落库以 (phone, answerSerial) 为单位整体覆盖，
// 便于查询“终端最近一次返回了哪些本地媒体”。
func (mp *JT808MsgProcessor) processMsg0802(ctx context.Context, data *model.ProcessData) error {
	mp.deps.Storage.Industry.Record(data.Incoming)
	result, ok := data.Incoming.(*model.Msg0802)
	if !ok || result.Header == nil {
		return nil
	}
	receivedAt := time.Now()
	records := make([]repository.MediaSearchRecord, 0, len(result.Items))
	for _, item := range result.Items {
		records = append(records, repository.MediaSearchRecord{
			MediaID:      item.MediaID,
			MediaType:    item.MediaType,
			ChannelID:    item.ChannelID,
			EventID:      item.EventID,
			AlarmSign:    item.Location.AlarmSign,
			StatusSign:   item.Location.StatusSign,
			Latitude:     item.Location.Latitude,
			Longitude:    item.Location.Longitude,
			Altitude:     item.Location.Altitude,
			Speed:        item.Location.Speed,
			Direction:    item.Location.Direction,
			LocationTime: parseMediaSearchLocationTime(item.Location.Time),
			ReceivedAt:   receivedAt,
		})
	}
	return mp.deps.Storage.MediaSearches.Replace(ctx, result.Header.PhoneNumber, result.AnswerSerial, records)
}

// parseMediaSearchLocationTime 按固定 GMT+8 解析内嵌位置体的 BCD 时间；
// 缺失或非法时返回零值，避免把平台接收时间误当成终端时间。
func parseMediaSearchLocationTime(value string) time.Time {
	if len(value) != 12 {
		return time.Time{}
	}
	parsed, err := time.ParseInLocation("060102150405", value, hex.ProtocolTimeZone)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// processMsg0805 把摄像头立即拍摄命令应答写入拍摄记录，
// mediaId 与 0x0801 落盘的文件通过 (phone, mediaId) 关联，查询时再判断文件是否已到达。
func (mp *JT808MsgProcessor) processMsg0805(ctx context.Context, data *model.ProcessData) error {
	reply, ok := data.Incoming.(*model.Msg0805)
	if !ok || reply.Header == nil {
		return nil
	}
	mp.deps.Storage.Industry.Record(reply)
	capture := &repository.MediaCapture{
		Phone:        reply.Header.PhoneNumber,
		RecordID:     strconv.FormatUint(uint64(reply.AnswerSerial), 10),
		AnswerSerial: reply.AnswerSerial,
		Result:       reply.Result,
		ReceivedAt:   time.Now(),
		Items:        make([]repository.MediaCaptureItem, 0, len(reply.MediaIDs)),
	}
	for _, mediaID := range reply.MediaIDs {
		capture.Items = append(capture.Items, repository.MediaCaptureItem{MediaID: mediaID})
	}
	return mp.deps.Storage.MediaCaptures.SaveCapture(ctx, capture)
}

// processMediaSignaling 记录终端上行的 1078 音视频信令状态，
// 供 API 查询与后续媒体接入使用；0x1206 同时落媒体元数据仓储。
func (mp *JT808MsgProcessor) processMediaSignaling(ctx context.Context, data *model.ProcessData) error {
	mp.deps.Storage.Industry.Record(data.Incoming)
	if notice, ok := data.Incoming.(*model.Msg1206); ok {
		return mp.deps.Storage.Media.SaveUploadNotice(ctx, notice.Header.PhoneNumber, notice)
	}
	return nil
}

// processMediaResourceList 记录 0x1205 音视频资源列表，并整包覆盖媒体元数据仓储。
func (mp *JT808MsgProcessor) processMediaResourceList(ctx context.Context, data *model.ProcessData) error {
	mp.deps.Storage.Industry.Record(data.Incoming)
	list, ok := data.Incoming.(*model.Msg1205)
	if !ok || list.Header == nil {
		return nil
	}
	return mp.deps.Storage.Media.ReplaceResourceList(ctx, list.Header.PhoneNumber, list)
}

// processMediaUploadTask 保存 0x9206 文件上传指令的元数据。
func (mp *JT808MsgProcessor) processMediaUploadTask(ctx context.Context, data *model.ProcessData) error {
	task, ok := data.Incoming.(*model.Msg9206)
	if !ok || task.Header == nil {
		return nil
	}
	return mp.deps.Storage.Media.SaveUploadTask(ctx, task.Header.PhoneNumber, task)
}

// processAttachmentSignaling 记录主链路上的苏标附件信令。
// 真正的附件接收、位图与 0x9212 应答在 internal/alarmserver 的独立端口完成。
func (mp *JT808MsgProcessor) processAttachmentSignaling(_ context.Context, data *model.ProcessData) error {
	if data == nil || data.Incoming == nil || data.Incoming.GetHeader() == nil {
		return nil
	}
	header := data.Incoming.GetHeader()
	log.Debug().Uint16("msg_id", header.MsgID).Str("phone", header.PhoneNumber).
		Msg("Attachment signaling received on main port, handled by alarm server")
	return nil
}

// processMsg8301 stores configured event items in the terminal simulator's
// event catalog according to the 0x8301 set type.
func (mp *JT808MsgProcessor) processMsg8301(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8301)
	mp.deps.Storage.Industry.ApplyEventSet(in.Header.PhoneNumber, in.SetType, in.Items)
	return nil
}

// processMsg8302 stores the issued question so the latest question and its
// candidate answers are available to the terminal simulator.
func (mp *JT808MsgProcessor) processMsg8302(_ context.Context, data *model.ProcessData) error {
	mp.deps.Storage.Industry.Record(data.Incoming)
	return nil
}

// processMsg8303 updates the terminal simulator's information on-demand menu
// catalog according to the 0x8303 set type.
func (mp *JT808MsgProcessor) processMsg8303(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8303)
	mp.deps.Storage.Industry.ApplyInfoMenuSet(in.Header.PhoneNumber, in.SetType, in.Items)
	return nil
}

// processMsg8304 stores the latest information-service message for a device.
func (mp *JT808MsgProcessor) processMsg8304(_ context.Context, data *model.ProcessData) error {
	mp.deps.Storage.Industry.Record(data.Incoming)
	return nil
}

// processMsg8401 updates the terminal simulator's phonebook according to the
// 0x8401 set type.
func (mp *JT808MsgProcessor) processMsg8401(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8401)
	mp.deps.Storage.Industry.ApplyPhonebookSet(in.Header.PhoneNumber, in.SetType, in.Items)
	return nil
}

// processMsg8500 stores the vehicle control flags so the 0x0500 response echoes
// them back to the platform.
func (mp *JT808MsgProcessor) processMsg8500(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8500)
	mp.deps.Storage.Industry.SetVehicleControl(in.Header.PhoneNumber, in.EchoControlFlags())
	return nil
}

func (mp *JT808MsgProcessor) processRegionResponse(_ context.Context, data *model.ProcessData) error {
	mp.deps.Runtime.RegionResponses.Deliver(data.Incoming.(*model.Msg0608))
	return nil
}

// processRegionRouteSet stores accepted area/route items in the terminal
// simulator's catalog so a subsequent 0x8608 query returns typed records.
func (mp *JT808MsgProcessor) processRegionRouteSet(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.MsgRegionRouteSet)
	mp.deps.Storage.Regions.ApplySet(in.Header.PhoneNumber, in)
	return nil
}

// processRegionRouteDelete removes area/route items from the catalog.
func (mp *JT808MsgProcessor) processRegionRouteDelete(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg860Delete)
	typ, ok := model.RegionRouteDeleteType(in.Header.MsgID)
	if !ok {
		return nil
	}
	mp.deps.Storage.Regions.Delete(in.Header.PhoneNumber, typ, in.IDs)
	return nil
}

// processRegionRouteQuery populates the outgoing 0x0608 with stored catalog
// records, filtered by the requested IDs.
func (mp *JT808MsgProcessor) processRegionRouteQuery(_ context.Context, data *model.ProcessData) error {
	query := data.Incoming.(*model.Msg8608)
	resp := data.Outgoing.(*model.Msg0608)
	stored := mp.deps.Storage.Regions.Query(query.Header.PhoneNumber, query.QueryType, query.IDs)
	resp.Circles = stored.Circles
	resp.Rectangles = stored.Rectangles
	resp.Polygons = stored.Polygons
	resp.Routes = stored.Routes
	return nil
}

func (mp *JT808MsgProcessor) processMediaSearchResponse(_ context.Context, data *model.ProcessData) error {
	query := data.Incoming.(*model.Msg8802)
	response := data.Outgoing.(*model.Msg0802)
	for _, media := range mp.deps.Storage.Industry.MediaUploads(query.Header.PhoneNumber) {
		if media.MultiMediaType != query.MediaType || (query.ChannelID != 0 && media.LogicChannelID != query.ChannelID) || (query.EventID != 0 && media.EventID != query.EventID) {
			continue
		}
		if len(media.GeoAlarmBody) != 28 {
			continue
		}
		location := model.Msg0200{}
		if err := location.Decode(&model.PacketData{Header: query.Header, Body: media.GeoAlarmBody}); err != nil {
			continue
		}
		if query.StartTime != "000000000000" && location.Time < query.StartTime {
			continue
		}
		if query.EndTime != "000000000000" && location.Time > query.EndTime {
			continue
		}
		response.Items = append(response.Items, model.MediaSearchItem{MediaID: media.MultiMediaID, MediaType: media.MultiMediaType, ChannelID: media.LogicChannelID, EventID: media.EventID, Location: location})
	}
	return nil
}

// sendOutgoingMsg encodes and writes a JT808 message through the session's
// pipeline so large payloads are fragmented automatically. It is used by
// terminal-simulator process functions to push extra messages (e.g. 0x0801
// uploads) in addition to the pipeline's own outgoing ack.
func sendOutgoingMsg(session *model.Session, msg model.JT808Msg) error {
	if session == nil || msg == nil {
		return errors.New("nil session or message")
	}
	ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, session)
	ctx = context.WithValue(ctx, model.ProcessDataCtxKey{}, &model.ProcessData{Outgoing: msg})
	return NewPipeline(session.Conn).ProcessConnWrite(ctx)
}

// mediaTimeMatches reports whether the 0x0200 location body of an uploaded
// media item falls inside the requested BCD time range. A zero boundary is
// treated as "no constraint".
func mediaTimeMatches(geoBody []byte, start, end string) bool {
	if len(geoBody) != 28 {
		return true
	}
	location := model.Msg0200{}
	if err := location.Decode(&model.PacketData{Body: geoBody}); err != nil {
		return true
	}
	if start != "000000000000" && location.Time < start {
		return false
	}
	if end != "000000000000" && location.Time > end {
		return false
	}
	return true
}

// processMsg8803 handles a platform "stored media upload" command on the
// terminal simulator side: it replays every previously captured media item
// that matches the requested type, channel, event and time range as 0x0801
// upload messages.
func (mp *JT808MsgProcessor) processMsg8803(ctx context.Context, data *model.ProcessData) error {
	cmd := data.Incoming.(*model.Msg8803)
	session, _ := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	if session == nil {
		return nil
	}
	phone := cmd.Header.PhoneNumber
	industry := mp.deps.Storage.Industry
	uploads := industry.MediaUploads(phone)
	for i := range uploads {
		media := uploads[i]
		if media.MultiMediaType != cmd.MediaType {
			continue
		}
		if cmd.ChannelID != 0 && media.LogicChannelID != cmd.ChannelID {
			continue
		}
		if cmd.EventID != 0 && media.EventID != cmd.EventID {
			continue
		}
		if !mediaTimeMatches(media.GeoAlarmBody, cmd.StartTime, cmd.EndTime) {
			continue
		}
		upload := &model.Msg0801{
			Header:              model.GenMsgHeader(&model.Device{Phone: phone, VersionDesc: cmd.Header.Attr.VersionDesc, ProtocolVersion: cmd.Header.ProtocolVersion}, 0x0801, session.GetNextSerialNum()),
			MultiMediaID:        media.MultiMediaID,
			MultiMediaType:      media.MultiMediaType,
			MultiMediaContainer: media.MultiMediaContainer,
			EventID:             media.EventID,
			LogicChannelID:      media.LogicChannelID,
			GeoAlarmBody:        append([]byte(nil), media.GeoAlarmBody...),
			FragmentData:        append([]byte(nil), media.FragmentData...),
		}
		if err := sendOutgoingMsg(session, upload); err != nil {
			log.Error().Err(err).Str("device", phone).Uint32("media_id", media.MultiMediaID).Msg("Failed to upload stored media on 0x8803")
		}
	}
	return nil
}

// processMsg8804 handles an audio recording command: it flips the simulator's
// recording flag and, when starting, emits a small number of audio 0x0801
// frames so the platform receives real media data.
func (mp *JT808MsgProcessor) processMsg8804(ctx context.Context, data *model.ProcessData) error {
	cmd := data.Incoming.(*model.Msg8804)
	session, _ := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	phone := cmd.Header.PhoneNumber
	industry := mp.deps.Storage.Industry
	industry.SetRecording(phone, cmd.Command == 1)
	if cmd.Command != 1 || session == nil {
		return nil
	}
	device := &model.Device{Phone: phone, VersionDesc: cmd.Header.Attr.VersionDesc, ProtocolVersion: cmd.Header.ProtocolVersion}
	frameCount := 3
	for i := 0; i < frameCount; i++ {
		upload := &model.Msg0801{
			Header:              model.GenMsgHeader(device, 0x0801, session.GetNextSerialNum()),
			MultiMediaID:        uint32(time.Now().UnixNano()/1e6) + uint32(i),
			MultiMediaType:      1, // audio
			MultiMediaContainer: 3, // WAV
			EventID:             0, // platform command
			LogicChannelID:      1,
			GeoAlarmBody:        make([]byte, 28),
			FragmentData:        []byte{0x52, 0x49, 0x46, 0x46, byte(i)},
		}
		if err := sendOutgoingMsg(session, upload); err != nil {
			log.Error().Err(err).Str("device", phone).Int("frame", i).Msg("Failed to upload recorded audio frame")
		}
	}
	return nil
}

// processMsg8805 handles a single-item media upload command by replaying the
// stored media item whose multimedia identifier matches the request.
func (mp *JT808MsgProcessor) processMsg8805(ctx context.Context, data *model.ProcessData) error {
	cmd := data.Incoming.(*model.Msg8805)
	session, _ := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	if session == nil {
		return nil
	}
	phone := cmd.Header.PhoneNumber
	industry := mp.deps.Storage.Industry
	for _, media := range industry.MediaUploads(phone) {
		if media.MultiMediaID != cmd.MediaID {
			continue
		}
		upload := &model.Msg0801{
			Header:              model.GenMsgHeader(&model.Device{Phone: phone, VersionDesc: cmd.Header.Attr.VersionDesc, ProtocolVersion: cmd.Header.ProtocolVersion}, 0x0801, session.GetNextSerialNum()),
			MultiMediaID:        media.MultiMediaID,
			MultiMediaType:      media.MultiMediaType,
			MultiMediaContainer: media.MultiMediaContainer,
			EventID:             media.EventID,
			LogicChannelID:      media.LogicChannelID,
			GeoAlarmBody:        append([]byte(nil), media.GeoAlarmBody...),
			FragmentData:        append([]byte(nil), media.FragmentData...),
		}
		if err := sendOutgoingMsg(session, upload); err != nil {
			log.Error().Err(err).Str("device", phone).Uint32("media_id", media.MultiMediaID).Msg("Failed to upload single media on 0x8805")
		}
		return nil
	}
	log.Warn().Str("device", phone).Uint32("media_id", cmd.MediaID).Msg("0x8805 requested media not found in simulator catalog")
	return nil
}

// processMsg8700 populates the outgoing 0x0700 with a typed GB/T 19056 response
// data block matching the platform command. Unsupported commands leave the
// response data empty.
func (mp *JT808MsgProcessor) processMsg8700(_ context.Context, data *model.ProcessData) error {
	cmd := data.Incoming.(*model.MsgRecordCommand)
	resp := data.Outgoing.(*model.Msg0700)
	now := time.Now().In(hex.ProtocolTimeZone)
	switch cmd.Command {
	case model.RecorderCmdCollectInfo:
		resp.DataBlock = &model.RecorderInfoResponse{
			Magic:           "GBT19056",
			StandardVersion: "21",
			RevisionNo:      0,
			RecorderTime:    hex.FormatTime(now),
			RecorderID:      make([]byte, 35),
			PlateNo:         "京A12345",
			PlateCategory:   "小型汽车",
			VIN:             "LSVNV2182E2100001",
			SerialNo:        "000000000001",
			PulseCoeff:      100,
			InstallTime:     "230101000000",
		}
	case model.RecorderCmdCollectFile:
		resp.DataBlock = &model.RecorderFileResponse{
			Items: []model.RecorderFileResponseItem{
				{Category: model.FileCategoryLog, FileLength: 4, FileData: []byte{0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	case model.RecorderCmdPositionTestOn, model.RecorderCmdPositionTestOff:
		resp.DataBlock = &model.RecorderPositionTestResponse{
			Time:    hex.FormatTime(now),
			GGAData: "$GPGGA,120000,3954.3265,N,11623.5432,E,1,08,1.0,45.3,M,,,,*47",
			RMCData: "$GPRMC,120000,A,3954.3265,N,11623.5432,E,0.0,0.0,291026,,,A*5C",
		}
	case model.RecorderCmdDigestTest:
		resp.DataBlock = &model.RecorderDigestTestResponse{
			RecorderTime: hex.FormatTime(now),
			RecorderID:   make([]byte, 35),
			Digest:       make([]byte, 32),
		}
	}
	return nil
}
