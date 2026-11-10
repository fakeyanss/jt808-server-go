package protocol

import (
	"sync"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// Owner 描述一条消息在 cluster 下的处理归属。
type Owner uint8

const (
	// OwnerGateway 表示只在网关跑：会话、分包、应答配对与协议头相关的处理。
	OwnerGateway Owner = iota
	// OwnerControl 表示只在管控跑：业务落库、档案与媒体元数据。
	OwnerControl
	// OwnerBoth 表示需要拆成协议半边（网关）与业务半边（管控）。
	OwnerBoth
)

func (o Owner) String() string {
	switch o {
	case OwnerGateway:
		return "gateway"
	case OwnerControl:
		return "control"
	case OwnerBoth:
		return "both"
	default:
		return "unknown"
	}
}

// Ownership 是 cluster 模式下的处理归属表。
//
// 归属只按“是否依赖活连接”划分，不按业务语义划分：需要会话、分包、
// 待应答配对的留在网关；纯业务落库的搬到管控；注册/鉴权拆成两半。
var Ownership = map[uint16]Owner{
	// 会话与传输：心跳、注销、通用应答、补传、重传与下行模拟。
	0x0001: OwnerGateway,
	0x0002: OwnerGateway,
	0x0003: OwnerGateway,
	0x0004: OwnerGateway,
	0x0005: OwnerGateway,
	0x8001: OwnerGateway,
	0x8003: OwnerGateway,
	0x8004: OwnerGateway,

	// 注册与鉴权：协议半边在网关，业务半边在管控。
	0x0100: OwnerBoth,
	0x0102: OwnerBoth,
	0x8100: OwnerGateway,

	// 查询应答：四类查询 broker 依赖本地会话，留在网关。
	0x0104: OwnerGateway,
	0x0107: OwnerGateway,
	0x0201: OwnerGateway,
	0x0608: OwnerGateway,

	// 定位、报警与行业上报：纯业务落库。
	0x0200: OwnerControl,
	0x0301: OwnerControl,
	0x0302: OwnerControl,
	0x0303: OwnerControl,
	0x0500: OwnerControl,
	0x0700: OwnerControl,
	0x0701: OwnerControl,
	0x0702: OwnerControl,
	0x0704: OwnerControl,
	0x0705: OwnerControl,
	0x0108: OwnerControl,
	0x0900: OwnerControl,
	0x0901: OwnerControl,

	// 多媒体与附件：元数据与文件写入都在管控。
	0x0800: OwnerControl,
	0x0801: OwnerControl,
	0x0802: OwnerControl,
	0x0805: OwnerControl,
	0x1205: OwnerControl,
	0x1206: OwnerControl,
	0x1210: OwnerControl,
	0x1211: OwnerControl,
	0x1212: OwnerControl,

	// 平台下行、1078 信令与模拟器行为：只在网关。
	0x8103: OwnerGateway,
	0x8104: OwnerGateway,
	0x8105: OwnerGateway,
	0x8106: OwnerGateway,
	0x8107: OwnerGateway,
	0x8108: OwnerGateway,
	0x8201: OwnerGateway,
	0x8202: OwnerGateway,
	0x8203: OwnerGateway,
	0x8204: OwnerGateway,
	0x8300: OwnerGateway,
	0x8301: OwnerGateway,
	0x8302: OwnerGateway,
	0x8303: OwnerGateway,
	0x8304: OwnerGateway,
	0x8400: OwnerGateway,
	0x8401: OwnerGateway,
	0x8500: OwnerGateway,
	0x8600: OwnerGateway,
	0x8601: OwnerGateway,
	0x8602: OwnerGateway,
	0x8603: OwnerGateway,
	0x8604: OwnerGateway,
	0x8605: OwnerGateway,
	0x8606: OwnerGateway,
	0x8607: OwnerGateway,
	0x8608: OwnerGateway,
	0x8700: OwnerGateway,
	0x8701: OwnerGateway,
	0x8702: OwnerGateway,
	0x8800: OwnerGateway,
	0x8801: OwnerGateway,
	0x8802: OwnerGateway,
	0x8803: OwnerGateway,
	0x8804: OwnerGateway,
	0x8805: OwnerGateway,
	0x8900: OwnerGateway,
	0x9101: OwnerGateway,
	0x9102: OwnerGateway,
	0x9105: OwnerGateway,
	0x9201: OwnerGateway,
	0x9202: OwnerGateway,
	0x9205: OwnerGateway,
	0x9206: OwnerGateway,
}

// BusinessHalves 是“两边都要”的消息在管控侧的业务半边函数名。
// registrydoc 用它校验控制面归属确实有业务半边。
var BusinessHalves = map[uint16]string{
	0x0100: "processRegisterBusiness",
	0x0102: "processAuthBusiness",
}

// ControlSide 表示消息是否有管控侧业务处理。
func ControlSide(msgID uint16) bool {
	owner, ok := Ownership[msgID]
	return ok && (owner == OwnerControl || owner == OwnerBoth)
}

// OwnershipOf 返回消息归属；未登记时 ok=false。
func OwnershipOf(msgID uint16) (Owner, bool) {
	owner, ok := Ownership[msgID]
	return owner, ok
}

var (
	bodyRegistryOnce sync.Once
	bodyRegistry     processOptions
)

// NewProcessData 按注册表构造一个消息处理上下文（含入站/出站类型）。
func (mp *JT808MsgProcessor) NewProcessData(msgID uint16) *model.ProcessData {
	bodyRegistryOnce.Do(func() {
		bodyRegistry = (&JT808MsgProcessor{}).initProcessOption()
	})
	if opt := bodyRegistry[msgID]; opt != nil && opt.genData != nil {
		return opt.genData()
	}
	return nil
}

// DecodeMessageBody 用注册表的入站类型解码消息体。
//
// 网关的 Decode 与管控执行器共用这一份实现，避免两边对同一消息体产生
// 不一致的结构。header 只提供手机号、流水号与协议版本等上下文，不做改写。
func DecodeMessageBody(msgID uint16, header *model.MsgHeader, body []byte) (model.JT808Msg, error) {
	bodyRegistryOnce.Do(func() {
		bodyRegistry = (&JT808MsgProcessor{}).initProcessOption()
	})
	opt := bodyRegistry[msgID]
	if opt == nil || opt.genData == nil {
		return nil, ErrMsgIDNotSupportted
	}
	data := opt.genData()
	if data.Incoming == nil {
		return nil, ErrMsgIDNotSupportted
	}
	packet := &model.PacketData{Header: header, Body: body}
	if header != nil {
		packet.Header = header
	}
	if err := data.Incoming.Decode(packet); err != nil {
		return nil, err
	}
	return data.Incoming, nil
}
