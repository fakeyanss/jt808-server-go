package protocol

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeYanss/jt808-server-go/internal/storage"
)

var (
	ErrMsgIDNotSupportted = errors.New("Msg id is not supportted")
	ErrNotAuthorized      = errors.New("Not authorized")
)

// 处理消息的Handler接口
type MsgProcessor interface {
	// 处理Packet包，生成Msg
	ProcessPacket(context.Context, *model.PacketData) (model.JT808Msg, error)

	// 处理Msg，生产Cmd
	ProcessMsg(context.Context, model.JT808Msg) (model.JT808Cmd, error)
}

// 处理jt808消息的Handler方法
type JT808MsgProcessor struct {
	options processOptions
}

// 消息处理方法调用表, <msgId, action>
type processOptions map[uint16]*action

type action struct {
	genData func() *model.ProcessData
	process func(context.Context, *model.ProcessData) error
}

// processor单例
var jt808MsgProcessorSingleton *JT808MsgProcessor
var processOnce sync.Once

func NewJT808MsgProcessor() *JT808MsgProcessor {
	processOnce.Do(func() {
		jt808MsgProcessorSingleton = &JT808MsgProcessor{
			options: initProcessOption(),
		}
	})
	return jt808MsgProcessorSingleton
}

// 表驱动，初始化消息处理方法组
func initProcessOption() processOptions {
	options := make(processOptions)
	options[0x0001] = &action{ // 通用应答
		genData: func() *model.ProcessData {
			return &model.ProcessData{Msg: &model.Msg0001{}}
		},
	}
	options[0x0002] = &action{ // 心跳
		genData: func() *model.ProcessData {
			return &model.ProcessData{Msg: &model.Msg0002{}, Cmd: &model.Cmd8001{}}
		},
		process: processMsg0002,
	}
	options[0x0003] = &action{ // 注销
		genData: func() *model.ProcessData {
			return &model.ProcessData{Msg: &model.Msg0003{}, Cmd: &model.Cmd8001{}}
		},
		process: processMsg0003,
	}
	options[0x0100] = &action{ // 注册
		genData: func() *model.ProcessData {
			return &model.ProcessData{Msg: &model.Msg0100{}, Cmd: &model.Cmd8100{}}
		},
		process: processMsg0100,
	}
	options[0x0102] = &action{ // 鉴权
		genData: func() *model.ProcessData {
			return &model.ProcessData{Msg: &model.Msg0102{}, Cmd: &model.Cmd8001{}}
		},
		process: processMsg0102,
	}
	// mc[0x0200] = &call{ // 位置信息上报
	// 	newMsg: func() model.JT808Msg { return &model.Msg0200{} },
	// 	newCmd: func() model.JT808Cmd { return &model.Cmd8001{} },
	// 	handle: handleMsg0200,
	// }

	return options
}

func (mp *JT808MsgProcessor) Process(ctx context.Context, pkt *model.PacketData) (*model.ProcessData, error) {
	msgID := pkt.Header.MsgID
	dataFunc := mp.options[msgID].genData
	if dataFunc == nil {
		return nil, ErrMsgIDNotSupportted
	}
	data := dataFunc()

	msg := data.Msg
	err := msg.Decode(pkt)
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

	if data.Cmd == nil {
		return nil, nil // 此类型msg不需要回复cmd
	}
	cmd := data.Cmd
	err = cmd.GenCmd(msg)
	if err != nil {
		return data, errors.Wrap(err, "Fail to generate jtcmd")
	}

	// print log of cmd content
	defer func() {
		if cmd == nil || log.Logger.GetLevel() != zerolog.DebugLevel {
			return
		}

		cmdJSON, _ := json.Marshal(cmd)
		session := ctx.Value(model.SessionCtxKey{}).(*model.Session)
		log.Debug().
			Str("id", session.ID).
			RawJSON("cmd", cmdJSON). // for debug
			Msg("Generating jt808 cmd.")
	}()

	processFunc := mp.options[msgID].process
	err = processFunc(ctx, data)
	if err != nil {
		return data, errors.Wrap(err, "Fail to process data")
	}
	return data, nil
}

// 收到心跳，应刷新终端缓存有效期
func processMsg0002(ctx context.Context, data *model.ProcessData) error {
	session := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	device, err := storage.GetDevice(session.ID)

	// 缓存不存在，说明连接已断开，需要返回错误
	if errors.Is(err, storage.ErrDeviceNotFound) {
		return errors.Wrap(err, "Fail to find device cache")
	}

	storage.CacheDevice(device)

	return nil
}

// 收到注销，应清除缓存，断开连接。
// 为避免连接TIMEWAIT，应等待对方主动关闭
func processMsg0003(ctx context.Context, data *model.ProcessData) error {
	session := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	storage.DelDevice(session.ID)
	return nil
}

// 收到注册，应校验设备ID，如果可注册，则缓存设备信息并返回鉴权码
func processMsg0100(ctx context.Context, data *model.ProcessData) error {
	session := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	device := &model.Device{
		ID:         session.ID,
		TransProto: session.GetTransProto(),
		Conn:       session.Conn,
		Authed:     false,
	}
	storage.CacheDevice(device)
	return nil
}

func processMsg0102(ctx context.Context, data *model.ProcessData) error {
	return nil
}

func handleMsg0200(ctx context.Context, data *model.ProcessData) error {
	return nil
}
