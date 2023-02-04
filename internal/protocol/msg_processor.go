package protocol

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeYanss/jt808-server-go/internal/storage"
)

var (
	ErrMsgIDNotSupportted = errors.New("Msg id is not supportted")
)

// 消息处理方法调用表
type processGroup struct {
	msgCalls map[uint16]*call // <msgId, call>
}

type call struct {
	newMsg func() model.JT808Msg                                       // newMsg必须定义
	newCmd func() model.JT808Cmd                                       // newCmd可以为空
	handle func(context.Context, model.JT808Msg, model.JT808Cmd) error // handle可以为空

	process  func(context.Context, *model.ProcessData) error
	callback func(context.Context, *model.ProcessData) error
}

// 表驱动，初始化消息处理方法组
func initHandleGroup() *processGroup {
	mc := make(map[uint16]*call)
	// mc[0x0001] = &call{ // 通用应答
	// newMsg: func() model.JT808Msg { return &model.Msg0001{} },
	// }
	mc[0x0002] = &call{ // 心跳
		newMsg: func() model.JT808Msg { return &model.Msg0002{} },
		newCmd: func() model.JT808Cmd { return &model.Cmd8001{} },
		handle: handleMsg0002,

		process: func(ctx context.Context, pd *model.ProcessData) error {

			return nil
		},
	}
	// mc[0x0003] = &call{ // 注销
	// 	newMsg: func() model.JT808Msg { return &model.Msg0003{} },
	// 	newCmd: func() model.JT808Cmd { return &model.Cmd8001{} },
	// 	handle: handleMsg0003,
	// }
	// mc[0x0100] = &call{ // 注册
	// 	newMsg: func() model.JT808Msg { return &model.Msg0100{} },
	// 	newCmd: func() model.JT808Cmd { return &model.Cmd8100{} },
	// 	handle: handleMsg0100,
	// }
	// mc[0x0102] = &call{ // 鉴权
	// 	newMsg: func() model.JT808Msg { return &model.Msg0102{} },
	// 	newCmd: func() model.JT808Cmd { return &model.Cmd8001{} },
	// 	handle: handleMsg0102,
	// }
	// mc[0x0200] = &call{ // 位置信息上报
	// 	newMsg: func() model.JT808Msg { return &model.Msg0200{} },
	// 	newCmd: func() model.JT808Cmd { return &model.Cmd8001{} },
	// 	handle: handleMsg0200,
	// }

	return &processGroup{msgCalls: mc}
}

// 处理消息的Handler接口
type MsgHandler interface {
	// 处理Packet包，生成Msg
	ProcessPacket(context.Context, *model.PacketData) (model.JT808Msg, error)

	// 处理Msg，生产Cmd
	ProcessMsg(context.Context, model.JT808Msg) (model.JT808Cmd, error)
}

// 处理jt808消息的Handler方法
type JT808MsgProcessor struct {
	pg *processGroup
}

// processor单例
var jt808MsgProcessorSingleton *JT808MsgProcessor
var handlerOnce sync.Once

func NewJT808MsgHandler() *JT808MsgProcessor {
	handlerOnce.Do(func() {
		jt808MsgProcessorSingleton = &JT808MsgProcessor{
			pg: initHandleGroup(),
		}
	})
	return jt808MsgProcessorSingleton
}

func (mp *JT808MsgProcessor) ProcessPacket(ctx context.Context, pkt *model.PacketData) (msg model.JT808Msg, err error) {
	msgID := pkt.Header.MsgID
	funcNewMsg := mp.pg.msgCalls[msgID].newMsg
	if funcNewMsg == nil {
		return nil, ErrMsgIDNotSupportted
	}
	msg = funcNewMsg()

	err = msg.Decode(pkt)
	if err != nil {
		return nil, errors.Wrap(err, "Fail to decode packet to jtmsg")
	}

	if log.Logger.GetLevel() == zerolog.DebugLevel {
		// print log of msg content
		session := ctx.Value(model.SessionCtxKey{}).(*model.Session)
		msgJSON, err := json.Marshal(msg)
		if err != nil {
			return nil, errors.Wrap(err, "Fail to serialize msg to json")
		}
		log.Debug().
			Str("id", session.ID).
			RawJSON("msg", msgJSON). // for debug
			Msg("Received jt808 msg.")
	}

	return msg, nil
}

func (mp *JT808MsgProcessor) ProcessMsg(ctx context.Context, msg model.JT808Msg) (cmd model.JT808Cmd, err error) {
	msgID := msg.GetHeader().MsgID
	funcNewCmd := mp.pg.msgCalls[msgID].newCmd

	defer func() {
		if cmd == nil || log.Logger.GetLevel() != zerolog.DebugLevel {
			return
		}

		cmdJSON, _ := json.Marshal(cmd)
		session := ctx.Value(model.SessionCtxKey{}).(*model.Session)
		log.Debug().
			Str("id", session.ID).
			RawJSON("cmd", cmdJSON).
			Msg("Sending jt808 cmd.")
	}()

	if funcNewCmd == nil {
		// 此类型msg不需要回复cmd
		return nil, nil
	}
	cmd = funcNewCmd()

	err = cmd.GenCmd(msg)
	if err != nil {
		return nil, errors.Wrap(err, "Fail to generate jtcmd")
	}

	funcHandle := mp.pg.msgCalls[msgID].handle
	if funcHandle == nil {
		// 此类型msg不需要handle处理
		return cmd, err
	}

	err = funcHandle(ctx, msg, cmd)
	if err != nil {
		return nil, errors.Wrap(err, "Fail to handle jtmsg")
	}

	return cmd, err
}

func (mp *JT808MsgProcessor) ProcessMsg1(ctx context.Context, pkt *model.PacketData) (*model.ProcessData, error) {
	return nil, nil
}

// 收到心跳，应刷新终端缓存有效期
func handleMsg0002(ctx context.Context, msg model.JT808Msg, cmd model.JT808Cmd) error {
	session := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	device, err := storage.GetDevice(session.ID)

	// 缓存不存在，说明连接已断开，需要返回错误
	if errors.Is(err, storage.ErrDeviceNotFound) {
	}

	device.LastComTime = time.Now().UnixMilli()
	storage.CacheDevice(device)

	return nil
}

// 收到注销，应清除缓存，断开连接
func handleMsg0003(ctx context.Context, msg model.JT808Msg, cmd model.JT808Cmd) error {
	return nil
}

// 收到注册，应校验设备ID，如果可注册，则缓存设备信息并返回鉴权码
func handleMsg0100(ctx context.Context, msg model.JT808Msg, cmd model.JT808Cmd) error {
	session := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	device := &model.Device{
		ID:          session.ID,
		TransProto:  session.GetTransProto(),
		Conn:        session.Conn,
		Authed:      false,
		LastComTime: time.Now().UnixMilli(),
	}
	storage.CacheDevice(device)
	return nil
}

func handleMsg0102(ctx context.Context, msg model.JT808Msg, cmd model.JT808Cmd) error {
	return nil
}

func handleMsg0200(ctx context.Context, msg model.JT808Msg, cmd model.JT808Cmd) error {
	return nil
}
