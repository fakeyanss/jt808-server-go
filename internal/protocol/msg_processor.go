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
	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
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

// 表驱动，初始化消息处理方法组。
// gateway 模式只保留终端发起的消息（消息ID < 0x8000），并把业务数据写入替换为旁路。
func initProcessOption(mode config.ServerMode) processOptions {
	options := make(processOptions)
	options[0x0001] = &action{ // 通用应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0001{}} // 无需回复
		},
		process: processMsg0001,
	}
	options[0x0002] = &action{ // 心跳
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0002{}, Outgoing: &model.Msg8001{}}
		},
		process: processMsg0002,
	}
	options[0x0003] = &action{ // 注销
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0003{}, Outgoing: &model.Msg8001{}}
		},
		process: processMsg0003,
	}
	options[0x0004] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg0004{}, Outgoing: &model.Msg8004{}}
	}}
	options[0x0005] = &action{genData: func() *model.ProcessData { return &model.ProcessData{Incoming: &model.Msg0005{}} }, process: processMsg0005}
	options[0x0100] = &action{ // 注册
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0100{}, Outgoing: &model.Msg8100{}}
		},
		process: processMsg0100,
	}
	options[0x0102] = &action{ // 鉴权
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0102{}, Outgoing: &model.Msg8001{}}
		},
		process: processMsg0102,
	}
	options[0x0104] = &action{ // 查询终端参数应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0104{}} // 无需回复
		},
		process: processMsg0104,
	}
	options[0x0107] = &action{genData: func() *model.ProcessData { return &model.ProcessData{Incoming: &model.Msg0107{}} }, process: processMsg0107}
	options[0x0201] = &action{genData: func() *model.ProcessData { return &model.ProcessData{Incoming: &model.Msg0201{}} }, process: processMsg0201}
	options[0x8202] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8202{}, Outgoing: &model.Msg0001{}}
	}, process: processMsg8202}
	options[0x8203] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8203{}, Outgoing: &model.Msg0001{}}
	}}
	options[0x8204] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8204{}, Outgoing: &model.Msg0001{}}
	}}
	options[0x0200] = &action{ // 位置信息上报
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0200{}, Outgoing: &model.Msg8001{}}
		},
		process: processMsg0200,
	}
	for _, messageID := range []uint16{0x8600, 0x8602, 0x8604, 0x8606} {
		options[messageID] = &action{genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.MsgRegionRouteSet{}, Outgoing: &model.Msg0001{}}
		}, process: processRegionRouteSet}
	}
	options[0x8608] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8608{}, Outgoing: &model.Msg0608{}}
	}, process: processRegionRouteQuery}
	options[0x0608] = &action{genData: func() *model.ProcessData { return &model.ProcessData{Incoming: &model.Msg0608{}} }, process: processRegionResponse}
	options[0x0702] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg0702{}, Outgoing: &model.Msg8001{}}
	}, process: processIndustryReport}
	options[0x0705] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg0705{}, Outgoing: &model.Msg8001{}}
	}, process: processIndustryReport}
	options[0x0700] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg0700{}, Outgoing: &model.Msg8001{}}
	}, process: processIndustryReport}
	options[0x8700] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.MsgRecordCommand{}, Outgoing: &model.Msg0700{}}
	}, process: processMsg8700}
	options[0x8701] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.MsgRecordCommand{}, Outgoing: &model.Msg0001{}}
	}}
	options[0x8702] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8702{}, Outgoing: &model.Msg0702{}}
	}}
	options[0x0800] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg0800{}, Outgoing: &model.Msg8001{}}
	}, process: processIndustryReport}
	options[0x0801] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg0801{}, Outgoing: &model.Msg8800{}}
	}, process: processIndustryReport}
	options[0x8800] = &action{genData: func() *model.ProcessData { return &model.ProcessData{Incoming: &model.Msg8800{}} }, process: processMsg8800}
	options[0x8801] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8801{}, Outgoing: &model.Msg0805{}}
	}}
	options[0x8802] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8802{}, Outgoing: &model.Msg0802{}}
	}, process: processMediaSearchResponse}
	options[0x0805] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg0805{}, Outgoing: &model.Msg8001{}}
	}, process: processIndustryReport}
	options[0x0802] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg0802{}, Outgoing: &model.Msg8001{}}
	}, process: processIndustryReport}
	options[0x8803] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8803{}, Outgoing: &model.Msg0001{}}
	}, process: processMsg8803}
	options[0x8804] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8804{}, Outgoing: &model.Msg0001{}}
	}, process: processMsg8804}
	options[0x8805] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8805{}, Outgoing: &model.Msg0001{}}
	}, process: processMsg8805}
	options[0x1205] = &action{ // 终端上传音视频资源列表
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg1205{}, Outgoing: &model.Msg8001{}} // 无需回复
		},
		process: processIndustryReport,
	}
	options[0x8001] = &action{ // 通用应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8001{}}
		},
		process: processMsg8001,
	}
	options[0x8003] = &action{ // 补传分包请求
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8003{}, Outgoing: &model.Msg0001{}}
		},
		process: processMsg8003,
	}
	options[0x8004] = &action{genData: func() *model.ProcessData { return &model.ProcessData{Incoming: &model.Msg8004{}} }}
	options[0x8100] = &action{ // 注册应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8100{}, Outgoing: &model.Msg0102{}}
		},
		process: processMsg8100,
	}
	options[0x8103] = &action{ // 查询终端参数
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8103{}, Outgoing: &model.Msg0001{}}
		},
		process: processMsg8103,
	}
	options[0x8104] = &action{ // 查询终端参数
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8104{}, Outgoing: &model.Msg0104{}}
		},
		process: processMsg8104,
	}
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
		process: processMsg8301,
	}
	options[0x0301] = &action{ // 事件上报
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0301{}, Outgoing: &model.Msg8001{}}
		},
		process: processIndustryReport,
	}
	options[0x8302] = &action{ // 提问下发
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8302{}, Outgoing: &model.Msg0001{}}
		},
		process: processMsg8302,
	}
	options[0x0302] = &action{ // 提问应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0302{}, Outgoing: &model.Msg8001{}}
		},
		process: processIndustryReport,
	}
	options[0x8303] = &action{ // 信息点播菜单设置
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8303{}, Outgoing: &model.Msg0001{}}
		},
		process: processMsg8303,
	}
	options[0x0303] = &action{ // 信息点播/取消
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0303{}, Outgoing: &model.Msg8001{}}
		},
		process: processIndustryReport,
	}
	options[0x8304] = &action{ // 信息服务
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8304{}, Outgoing: &model.Msg0001{}}
		},
		process: processMsg8304,
	}
	options[0x8400] = &action{ // 电话回拨
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8400{}, Outgoing: &model.Msg0001{}}
		},
		process: processIndustryReport,
	}
	options[0x8401] = &action{ // 设置电话本
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8401{}, Outgoing: &model.Msg0001{}}
		},
		process: processMsg8401,
	}
	options[0x8108] = &action{ // 终端升级
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8108{}, Outgoing: &model.Msg0001{}}
		},
		process: processIndustryReport,
	}
	options[0x0108] = &action{ // 升级结果通知
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0108{}, Outgoing: &model.Msg8001{}}
		},
		process: processIndustryReport,
	}
	options[0x8900] = &action{ // 数据下行透传
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8900{}, Outgoing: &model.Msg0001{}}
		},
		process: processIndustryReport,
	}
	options[0x0900] = &action{ // 数据上行透传
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0900{}, Outgoing: &model.Msg8001{}}
		},
		process: processIndustryReport,
	}
	options[0x0901] = &action{ // 数据压缩上报
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0901{}, Outgoing: &model.Msg8001{}}
		},
		process: processIndustryReport,
	}
	options[0x8500] = &action{ // 车辆控制
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg8500{}, Outgoing: &model.Msg0500{}}
		},
		process: processMsg8500,
	}
	options[0x0500] = &action{ // 车辆控制应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0500{}, Outgoing: &model.Msg8001{}}
		},
		process: processIndustryReport,
	}
	options[0x0701] = &action{ // 电子运单上报
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0701{}, Outgoing: &model.Msg8001{}}
		},
		process: processIndustryReport,
	}
	options[0x0704] = &action{ // 批量定位数据上传
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg0704{}, Outgoing: &model.Msg8001{}}
		},
		process: processIndustryReport,
	}
	for _, messageID := range []uint16{0x8601, 0x8603, 0x8605, 0x8607} {
		options[messageID] = &action{genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg860Delete{}, Outgoing: &model.Msg0001{}}
		}, process: processRegionRouteDelete}
	}
	options[0x8106] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8106{}, Outgoing: &model.Msg0104{}}
	}, process: processMsg8106}
	options[0x8107] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8107{}, Outgoing: &model.Msg0107{}}
	}, process: processMsg8107}
	options[0x8201] = &action{genData: func() *model.ProcessData {
		return &model.ProcessData{Incoming: &model.Msg8201{}, Outgoing: &model.Msg0201{}}
	}, process: processMsg8201}
	options[0x9205] = &action{ // 查询终端音视频资源列表
		genData: func() *model.ProcessData {
			return &model.ProcessData{Incoming: &model.Msg9205{}, Outgoing: &model.Msg1205{}}
		},
		process: processMsg9205,
	}

	if mode == config.ServerModeGateway {
		filterGatewayOptions(options)
	}

	return options
}

// gateway 模式只保留终端发起的消息。0x8xxx 属于平台侧下发/模拟器处理，
// 接入网关不会收到，注册后也不参与业务回调。
func filterGatewayOptions(options processOptions) {
	for msgID := range options {
		if msgID >= 0x8000 {
			delete(options, msgID)
		}
	}
	// 仅 standalone 才需要维护的业务缓存，在网关模式改为只保留协议应答。
	for _, msgID := range gatewayBypassProcessMsgIDs {
		if act, ok := options[msgID]; ok {
			act.process = nil
		}
	}
	// 通用应答仍需清理会话在途消息，但不能写报警缓存。
	if act, ok := options[0x0001]; ok {
		act.process = gatewayProcessMsg0001
	}
	// 位置/报警不写设备、定位、报警缓存，由 dispatchHooks 外发。
	if act, ok := options[0x0200]; ok {
		act.process = nil
	}
}

// gatewayBypassProcessMsgIDs 列出 gateway 模式不落盘的业务消息。
// 这些消息仍会生成协议应答，但不再写入 internal/storage。
var gatewayBypassProcessMsgIDs = []uint16{
	0x0104, 0x0107, 0x0108,
	0x0201, 0x0301, 0x0302, 0x0303, 0x0500, 0x0608,
	0x0700, 0x0701, 0x0702, 0x0704, 0x0705,
	0x0800, 0x0801, 0x0802, 0x0805,
	0x0900, 0x0901, 0x1205,
}

// gatewayProcessMsg0001 只确认会话在途消息，不触碰报警缓存。
func gatewayProcessMsg0001(ctx context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0001)
	if session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session); ok && session != nil {
		session.Acknowledge(in.AnswerMessageID, in.AnswerSerialNumber)
	}
	return nil
}

// 处理jt808消息的Handler方法
type JT808MsgProcessor struct {
	hooks   actionpkg.Hooks
	options processOptions
}

// NewJT808MsgProcessor 创建消息处理器。
// mode 为 gateway 时只处理终端发起消息，业务数据通过 hooks 外发；
// hooks 为 nil 时使用 action.Noop，保证 standalone 行为与历史版本一致。
func NewJT808MsgProcessor(mode config.ServerMode, hooks actionpkg.Hooks) *JT808MsgProcessor {
	if mode != config.ServerModeGateway {
		mode = config.ServerModeStandalone
	}
	if hooks == nil {
		hooks = actionpkg.Noop{}
	}
	return &JT808MsgProcessor{
		hooks:   hooks,
		options: initProcessOption(mode),
	}
}

func (mp *JT808MsgProcessor) Process(ctx context.Context, pkt *model.PacketData) (*model.ProcessData, error) {
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
		session.AcknowledgeResponse(in.GetHeader().MsgID, in.GetHeader().PhoneNumber)
	}

	if log.Logger.GetLevel() == zerolog.DebugLevel {
		// print log of msg content
		session := ctx.Value(model.SessionCtxKey{}).(*model.Session)
		inJSON, err := json.Marshal(redactIncomingForLog(in))
		if err != nil {
			return nil, errors.Wrap(err, "Fail to serialize incoming msg to json")
		}
		// for debug
		log.Debug().Str("id", session.ID).Str("RawMsgID", fmt.Sprintf("0x%04x", in.GetHeader().MsgID)).RawJSON("incoming", inJSON).Msg("Received jt808 msg.")
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
			session := ctx.Value(model.SessionCtxKey{}).(*model.Session)
			// for debug
			log.Debug().Str("id", session.ID).Str("RawMsgID", fmt.Sprintf("0x%04x", out.GetHeader().MsgID)).RawJSON("outgoing", outJSON).
				Msg("Generating jt808 outgoing msg.")
		}()
	}

	// 对消息按类别做特殊处理
	processFunc := act.process
	if processFunc != nil {
		err = processFunc(ctx, data)
		if err != nil {
			return data, errors.Wrap(err, "Fail to process data")
		}
	}

	// 生命周期钩子只做旁路通知，与是否写入缓存无关。
	mp.dispatchHooks(ctx, data)

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
		mp.hooks.OnAlarm(ctx, actionpkg.AlarmEvent{
			Phone:     in.Header.PhoneNumber,
			Serial:    in.Header.SerialNumber,
			AlarmSign: in.AlarmSign,
			Latitude:  in.Latitude,
			Longitude: in.Longitude,
			Time:      in.Time,
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

// 收到心跳，应刷新终端缓存有效期
func processMsg0002(_ context.Context, data *model.ProcessData) error {
	cache := storage.GetDeviceCache()
	device, err := cache.GetDeviceByPhone(data.Incoming.GetHeader().PhoneNumber)

	// 缓存不存在，说明设备不合法，需要返回错误，让服务层处理关闭
	if errors.Is(err, storage.ErrDeviceNotFound) {
		return errors.Wrapf(err, "Fail to find device cache, phoneNumber=%s", data.Incoming.GetHeader().PhoneNumber)
	}

	device.LastestComTime = time.Now()
	cache.CacheDevice(device)

	return nil
}

// 收到注销，应清除缓存，断开连接。
func processMsg0003(_ context.Context, data *model.ProcessData) error {
	cache := storage.GetDeviceCache()
	device, err := cache.GetDeviceByPhone(data.Incoming.GetHeader().PhoneNumber)
	// 缓存不存在，说明设备不合法，需要返回错误，让服务层处理关闭
	if errors.Is(err, storage.ErrDeviceNotFound) {
		return errors.Wrapf(err, "Fail to find device cache, phoneNumber=%s", data.Incoming.GetHeader().PhoneNumber)
	}
	// 取消定时任务
	timer := NewKeepaliveTimer()
	timer.Cancel(device.Phone)
	// 清楚缓存
	cache.DelDeviceByPhone(device.Phone)
	// 为避免连接TIMEWAIT，应等待对方主动关闭
	return nil
}

// 收到注册，应校验设备ID，如果可注册，则缓存设备信息并返回鉴权码
func processMsg0100(ctx context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0100)

	cache := storage.GetDeviceCache()
	// 校验注册逻辑
	out := data.Outgoing.(*model.Msg8100)
	// 车辆已被注册
	if cache.HasPlate(in.PlateNumber) {
		out.Result = model.ResCarAlreadyRegister
		return nil
	}
	// 终端已被注册
	if cache.HasPhone(in.Header.PhoneNumber) {
		out.Result = model.ResDeviceAlreadyRegister
		return nil
	}

	session := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	device := model.NewDevice(in, session)
	out.AuthCode = genAuthCode(device) // 设置鉴权码

	cache.CacheDevice(device)

	timer := NewKeepaliveTimer()
	timer.Register(device.Phone)
	return nil
}

// 收到鉴权，应校验鉴权token
func processMsg0102(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0102)

	cache := storage.GetDeviceCache()
	device, err := cache.GetDeviceByPhone(in.Header.PhoneNumber)
	// 缓存不存在，说明设备不合法，需要返回错误，让服务层处理关闭
	if errors.Is(err, storage.ErrDeviceNotFound) {
		return errors.Wrapf(err, "Fail to find device cache, phoneNumber=%s", in.Header.PhoneNumber)
	}

	out := data.Outgoing.(*model.Msg8001)
	// 校验鉴权逻辑
	if in.AuthCode != genAuthCode(device) {
		out.Result = model.ResultFail
		// 取消定时任务
		timer := NewKeepaliveTimer()
		timer.Cancel(device.Phone)
		// 删除设备缓存
		cache.DelDeviceByPhone(device.Phone)
	} else {
		// 鉴权通过
		device.Status = model.DeviceStatusOnline
		device.LastestComTime = time.Now()
		device.AuthCode = in.AuthCode
		device.IMEI = in.IMEI
		device.SoftwareVersion = in.SoftwareVersion
		cache.CacheDevice(device)
	}

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
func processMsg0104(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0104)
	if in.Parameters != nil {
		storage.GetDeviceParamsCache().CacheDeviceParams(in.Parameters)
	}
	storage.GetParamResponseBroker().Deliver(in)
	return nil
}

func processMsg0107(_ context.Context, data *model.ProcessData) error {
	storage.GetPropertyResponseBroker().Deliver(data.Incoming.(*model.Msg0107))
	return nil
}

func processMsg8201(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8201)
	out := data.Outgoing.(*model.Msg0201)
	geo, err := storage.GetGeoCache().GetGeoLatestByPhone(in.Header.PhoneNumber)
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
		return storage.ErrGisNotFound
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

func processMsg0201(_ context.Context, data *model.ProcessData) error {
	storage.GetLocationResponseBroker().Deliver(data.Incoming.(*model.Msg0201))
	return nil
}

func processMsg8202(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8202)
	storage.GetLocationTrackingCache().Set(in.Header.PhoneNumber, in.TimeInterval, in.ValidityPeriod)
	return nil
}

// 收到位置信息汇报，回复通用应答
func processMsg0200(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0200)

	cache := storage.GetDeviceCache()
	device, err := cache.GetDeviceByPhone(in.Header.PhoneNumber)
	// 缓存不存在，说明设备不合法，需要返回错误，让服务层处理关闭
	if errors.Is(err, storage.ErrDeviceNotFound) {
		return errors.Wrapf(err, "Fail to find device cache, phoneNumber=%s", in.Header.PhoneNumber)
	}
	storage.GetAlarmCache().Record(in.Header.PhoneNumber, in.Header.SerialNumber, in.AlarmSign, in.Latitude, in.Longitude, in.Time)

	// 解析状态位编码
	dg := &model.DeviceGeo{}
	err = dg.Decode(device.Phone, in)
	if err != nil {
		return errors.Wrapf(err, "Fail to decode device geo, phoneNumber=%s", device.Phone)
	}

	if dg.Geo.ACCStatus == 0 { // ACC关闭，设备休眠
		device.Status = model.DeviceStatusSleeping
		device.LastestComTime = time.Now()
		cache.CacheDevice(device)
	}

	geoCache := storage.GetGeoCache()
	rb := geoCache.GetGeoRingByPhone(device.Phone)
	rb.Write(dg)

	return nil
}

func processMsg0001(ctx context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg0001)
	if session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session); ok && session != nil {
		session.Acknowledge(in.AnswerMessageID, in.AnswerSerialNumber)
	}
	if in.AnswerMessageID == 0x8203 {
		storage.GetAlarmCache().ConfirmByCommand(in.Header.PhoneNumber, in.AnswerSerialNumber)
	}
	return nil
}

func processMsg8001(ctx context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8001)
	if session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session); ok && session != nil {
		session.Acknowledge(in.AnswerMessageID, in.AnswerSerialNumber)
	}
	if in.AnswerMessageID != 0x0102 {
		return nil
	}
	// 收到8001消息，说明此时是作为终端设备
	if in.Result == model.ResultSuccess {
		// 回复成功，说明之前注册成功，为方便后续处理，将设备状态改为Online并缓存
		cache := storage.GetDeviceCache()
		device, err := cache.GetDeviceByPhone(in.Header.PhoneNumber)
		if errors.Is(err, storage.ErrDeviceNotFound) {
			return nil
		}
		if device.Status != model.DeviceStatusOffline {
			return nil
		}

		// 仅在未注册成功时执行一次
		device.Status = model.DeviceStatusOnline
		device.LastestComTime = time.Now()
		cache.CacheDevice(device)
	}

	return nil
}

// processMsg8003 is used by the client simulator to replay requested fragment
// frames. A production terminal handles the same downlink command itself.
func processMsg8003(ctx context.Context, data *model.ProcessData) error {
	request := data.Incoming.(*model.Msg8003)
	session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	if !ok || session == nil {
		return nil
	}
	frames := storage.GetSentSegmentFrames(request.Header.PhoneNumber, request.OriginalSerialNumber, request.PacketIDs)
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
func processMsg0005(ctx context.Context, data *model.ProcessData) error {
	request := data.Incoming.(*model.Msg0005)
	session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	if !ok || session == nil {
		return nil
	}
	retransmitCachedSegments(session, request.Header.PhoneNumber, request.OriginalSerialNumber, request.PacketIDs)
	return nil
}

// processMsg8800 replays media upload fragments retained by the terminal
// simulator. JT808 transport-level 0x8003 recovery remains independent.
func processMsg8800(ctx context.Context, data *model.ProcessData) error {
	request := data.Incoming.(*model.Msg8800)
	if len(request.RetransmitIDs) == 0 {
		return nil
	}
	session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	if !ok || session == nil {
		return nil
	}
	serial, ok := storage.GetMediaSegmentSerial(request.Header.PhoneNumber, request.MultiMediaID)
	if !ok {
		log.Warn().Str("device", request.Header.PhoneNumber).Uint32("media_id", request.MultiMediaID).Msg("No cached media fragments for retransmission request")
		return nil
	}
	retransmitCachedSegments(session, request.Header.PhoneNumber, serial, request.RetransmitIDs)
	return nil
}

func retransmitCachedSegments(session *model.Session, phone string, originalSerial uint16, packetIDs []uint16) {
	frames := storage.GetSentSegmentFrames(phone, originalSerial, packetIDs)
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
func processMsg8100(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8100)
	out := data.Outgoing.(*model.Msg0102)

	cache := storage.GetDeviceCache()
	device, err := cache.GetDeviceByPhone(in.Header.PhoneNumber)
	if errors.Is(err, storage.ErrDeviceNotFound) {
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
func processMsg8103(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8103)
	paramCache := storage.GetDeviceParamsCache()
	params, err := paramCache.GetDeviceParamsByPhone(in.GetHeader().PhoneNumber)
	if errors.Is(err, storage.ErrDeviceParamsNotFound) {
		params = in.Parameters
	} else {
		params.Update(in.Parameters)
	}
	paramCache.CacheDeviceParams(params)

	return nil
}

// 收到查询终端参数请求，回复终端参数(此时是作为client进程)
func processMsg8104(_ context.Context, data *model.ProcessData) error {
	// todo: generate by config
	out := data.Outgoing.(*model.Msg0104)
	paramCache := storage.GetDeviceParamsCache()
	params, err := paramCache.GetDeviceParamsByPhone(out.GetHeader().PhoneNumber)
	if errors.Is(err, storage.ErrDeviceParamsNotFound) {
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
		paramCache.CacheDeviceParams(out.Parameters)
	} else {
		out.Parameters = params
	}

	return nil
}

func processMsg8106(ctx context.Context, data *model.ProcessData) error {
	if err := processMsg8104(ctx, data); err != nil {
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

func processMsg8107(_ context.Context, data *model.ProcessData) error {
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

func processMsg9205(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg9205)
	out := data.Outgoing.(*model.Msg1205)
	out.Items = nil
	for _, media := range storage.GetIndustryCache().MediaUploads(in.Header.PhoneNumber) {
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
	loc, err := time.Parse("060102150405", t)
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

func processIndustryReport(_ context.Context, data *model.ProcessData) error {
	storage.GetIndustryCache().Record(data.Incoming)
	return nil
}

// processMsg8301 stores configured event items in the terminal simulator's
// event catalog according to the 0x8301 set type.
func processMsg8301(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8301)
	storage.GetIndustryCache().ApplyEventSet(in.Header.PhoneNumber, in.SetType, in.Items)
	return nil
}

// processMsg8302 stores the issued question so the latest question and its
// candidate answers are available to the terminal simulator.
func processMsg8302(_ context.Context, data *model.ProcessData) error {
	storage.GetIndustryCache().Record(data.Incoming)
	return nil
}

// processMsg8303 updates the terminal simulator's information on-demand menu
// catalog according to the 0x8303 set type.
func processMsg8303(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8303)
	storage.GetIndustryCache().ApplyInfoMenuSet(in.Header.PhoneNumber, in.SetType, in.Items)
	return nil
}

// processMsg8304 stores the latest information-service message for a device.
func processMsg8304(_ context.Context, data *model.ProcessData) error {
	storage.GetIndustryCache().Record(data.Incoming)
	return nil
}

// processMsg8401 updates the terminal simulator's phonebook according to the
// 0x8401 set type.
func processMsg8401(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8401)
	storage.GetIndustryCache().ApplyPhonebookSet(in.Header.PhoneNumber, in.SetType, in.Items)
	return nil
}

// processMsg8500 stores the vehicle control flags so the 0x0500 response echoes
// them back to the platform.
func processMsg8500(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg8500)
	storage.GetIndustryCache().SetVehicleControl(in.Header.PhoneNumber, in.ControlFlags)
	return nil
}

func processRegionResponse(_ context.Context, data *model.ProcessData) error {
	storage.GetRegionResponseBroker().Deliver(data.Incoming.(*model.Msg0608))
	return nil
}

// processRegionRouteSet stores accepted area/route items in the terminal
// simulator's catalog so a subsequent 0x8608 query returns typed records.
func processRegionRouteSet(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.MsgRegionRouteSet)
	storage.GetRegionCatalog().ApplySet(in.Header.PhoneNumber, in)
	return nil
}

// processRegionRouteDelete removes area/route items from the catalog.
func processRegionRouteDelete(_ context.Context, data *model.ProcessData) error {
	in := data.Incoming.(*model.Msg860Delete)
	typ, ok := model.RegionRouteDeleteType(in.Header.MsgID)
	if !ok {
		return nil
	}
	storage.GetRegionCatalog().Delete(in.Header.PhoneNumber, typ, in.IDs)
	return nil
}

// processRegionRouteQuery populates the outgoing 0x0608 with stored catalog
// records, filtered by the requested IDs.
func processRegionRouteQuery(_ context.Context, data *model.ProcessData) error {
	query := data.Incoming.(*model.Msg8608)
	resp := data.Outgoing.(*model.Msg0608)
	stored := storage.GetRegionCatalog().Query(query.Header.PhoneNumber, query.QueryType, query.IDs)
	resp.Circles = stored.Circles
	resp.Rectangles = stored.Rectangles
	resp.Polygons = stored.Polygons
	resp.Routes = stored.Routes
	return nil
}

func processMediaSearchResponse(_ context.Context, data *model.ProcessData) error {
	query := data.Incoming.(*model.Msg8802)
	response := data.Outgoing.(*model.Msg0802)
	for _, media := range storage.GetIndustryCache().MediaUploads(query.Header.PhoneNumber) {
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
func processMsg8803(ctx context.Context, data *model.ProcessData) error {
	cmd := data.Incoming.(*model.Msg8803)
	session, _ := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	if session == nil {
		return nil
	}
	phone := cmd.Header.PhoneNumber
	industry := storage.GetIndustryCache()
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
func processMsg8804(ctx context.Context, data *model.ProcessData) error {
	cmd := data.Incoming.(*model.Msg8804)
	session, _ := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	phone := cmd.Header.PhoneNumber
	industry := storage.GetIndustryCache()
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
func processMsg8805(ctx context.Context, data *model.ProcessData) error {
	cmd := data.Incoming.(*model.Msg8805)
	session, _ := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	if session == nil {
		return nil
	}
	phone := cmd.Header.PhoneNumber
	industry := storage.GetIndustryCache()
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
func processMsg8700(_ context.Context, data *model.ProcessData) error {
	cmd := data.Incoming.(*model.MsgRecordCommand)
	resp := data.Outgoing.(*model.Msg0700)
	now := time.Now()
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
