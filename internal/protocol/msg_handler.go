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
)

// 消息处理组
type handlerGroup struct {
	msgCall map[uint16]*call // <msgId, call>
}

// 消息处理方法
type call struct {
	newMsg func() model.JT808Msg                                       // newMsg必须定义
	newCmd func() model.JT808Cmd                                       // newCmd可以为空
	handle func(context.Context, model.JT808Msg, model.JT808Cmd) error // handle可以为空
}

// 表驱动，初始化消息处理方法组
func initHandleGroup() *handlerGroup {
	mc := make(map[uint16]*call)
	mc[0x0001] = &call{ // 通用应答
		newMsg: func() model.JT808Msg { return &model.Msg0001{} },
	}
	mc[0x0002] = &call{ // 心跳
		newMsg: func() model.JT808Msg { return &model.Msg0002{} },
		newCmd: func() model.JT808Cmd { return &model.Cmd8001{} },
		handle: handleMsg0002,
	}
	mc[0x0003] = &call{ // 注销
		newMsg: func() model.JT808Msg { return &model.Msg0003{} },
		newCmd: func() model.JT808Cmd { return &model.Cmd8001{} },
		handle: handleMsg0003,
	}
	mc[0x0100] = &call{ // 注册
		newMsg: func() model.JT808Msg { return &model.Msg0100{} },
		newCmd: func() model.JT808Cmd { return &model.Cmd8100{} },
		handle: handleMsg0100,
	}
	mc[0x0102] = &call{ // 鉴权
		newMsg: func() model.JT808Msg { return &model.Msg0102{} },
		newCmd: func() model.JT808Cmd { return &model.Cmd8001{} },
		handle: handleMsg0102,
	}
	mc[0x0200] = &call{ // 位置信息上报
		newMsg: func() model.JT808Msg { return &model.Msg0200{} },
		newCmd: func() model.JT808Cmd { return &model.Cmd8001{} },
		handle: handleMsg0200,
	}

	return &handlerGroup{msgCall: mc}
}

// 处理消息的Handler接口
type MsgHandler interface {
	// 处理Packet包，生成Msg
	ProcessPacket(context.Context, *model.Packet) (model.JT808Msg, error)

	// 处理Msg，生产Cmd
	ProcessMsg(context.Context, model.JT808Msg) (model.JT808Cmd, error)
}

// 处理jt808消息的Handler方法
type JT808MsgHandler struct {
	hg *handlerGroup
}

// handler单例
var jt808MsgHandler *JT808MsgHandler
var handlerOnce sync.Once

func NewJT808MsgHandler() *JT808MsgHandler {
	handlerOnce.Do(func() {
		jt808MsgHandler = &JT808MsgHandler{
			hg: initHandleGroup(),
		}
	})
	return jt808MsgHandler
}

func (h *JT808MsgHandler) ProcessPacket(ctx context.Context, pkt *model.Packet) (msg model.JT808Msg, err error) {
	msgID := pkt.Header.MsgID
	funcNewMsg := h.hg.msgCall[msgID].newMsg
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

func (h *JT808MsgHandler) ProcessMsg(ctx context.Context, msg model.JT808Msg) (cmd model.JT808Cmd, err error) {
	msgID := msg.GetHeader().MsgID
	funcNewCmd := h.hg.msgCall[msgID].newCmd

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

	funcHandle := h.hg.msgCall[msgID].handle
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

func handleMsg0002(ctx context.Context, msg model.JT808Msg, cmd model.JT808Cmd) error {
	return nil
}

func handleMsg0003(ctx context.Context, msg model.JT808Msg, cmd model.JT808Cmd) error {
	return nil
}

func handleMsg0100(ctx context.Context, msg model.JT808Msg, cmd model.JT808Cmd) error {
	session := ctx.Value(model.SessionCtxKey{}).(*model.Session)
	device := &model.Device{
		ID: session.ID,
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
