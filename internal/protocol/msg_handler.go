package protocol

import (
	"fmt"

	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
)

type MsgHandler interface {
	ProcessPacket(*model.Packet) (model.JT808Msg, error)
	ProcessMsg(model.JT808Msg) (model.JT808Cmd, error)
}

type JT808MsgHandler struct {
}

func NewJT808MsgHandler() *JT808MsgHandler {
	return &JT808MsgHandler{}
}

func (h *JT808MsgHandler) ProcessPacket(pkt *model.Packet) (msg model.JT808Msg, err error) {
	switch msgID := pkt.Header.MsgID; msgID {
	case 0x0001:
		msg = &model.Msg0001{}
	case 0x0002:
		msg = &model.Msg0002{}
	case 0x0003:
		msg = &model.Msg0003{}
	case 0x0100:
		msg = &model.Msg0100{}
	case 0x0102:
		msg = &model.Msg0102{}
	case 0x0200:
		msg = &model.Msg0200{}
	default:
		err = fmt.Errorf("unexpected msgID %v", msgID)
	}

	if err != nil {
		return
	}

	err = msg.Decode(pkt)
	if err != nil {
		return
	}

	return
}

func (h *JT808MsgHandler) ProcessMsg(msg model.JT808Msg) (cmd model.JT808Cmd, err error) {
	switch t := msg.(type) {
	case *model.Msg0001:
		return
	case *model.Msg0002:
		cmd = &model.Cmd8001{}
	case *model.Msg0003:
		cmd = &model.Cmd8001{}
	case *model.Msg0100:
		cmd = &model.Cmd8100{}
	case *model.Msg0200:
		cmd = &model.Cmd8001{}
	default:
		err = fmt.Errorf("unexpected type %T", t)
	}

	if err != nil {
		return
	}

	err = cmd.GenCmd(msg)
	if err != nil {
		return
	}

	return
}
