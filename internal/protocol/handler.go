package protocol

import (
	"fmt"

	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
)

type MsgHandler interface {
	ProcessPacket(*Packet) (model.JT808Msg, error)
	ProcessMsg(model.JT808Msg) (model.JT808Cmd, error)
}

type JT808MsgHandler struct {
}

func NewJT808MsgHandler() *JT808MsgHandler {
	return &JT808MsgHandler{}
}

func (h *JT808MsgHandler) ProcessPacket(pkt *Packet) (msg model.JT808Msg, err error) {
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
	default:
		err = fmt.Errorf("unexpected msgID %v", msgID)
	}

	if err != nil {
		return
	}

	err = msg.Decode(pkt.Body)
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
		m := msg.(*model.Msg0002)
		cmd, err = h.genCmd8001(&(m.MsgHeader))
	case *model.Msg0100:
		m := msg.(*model.Msg0100)
		cmd, err = h.genCmd8100(m)
	default:
		err = fmt.Errorf("unexpected type %T", t)
	}
	return
}

func (h *JT808MsgHandler) genCmd8001(header *model.MsgHeader) (cmd model.JT808Cmd, err error) {
	cmd = &model.Cmd8001{}
	c := cmd.(*model.Cmd8001)
	c.AnswerSerialNumber = header.SerialNumber
	c.AnswerMessageID = header.MsgID
	c.Result = 0

	c.MsgHeader = *header
	c.MsgID = 0x8001

	return
}

func (h *JT808MsgHandler) genCmd8100(msg *model.Msg0100) (cmd model.JT808Cmd, err error) {
	cmd = &model.Cmd8100{}
	c := cmd.(*model.Cmd8100)
	c.AnswerSerialNumber = msg.SerialNumber
	c.Result = 0
	c.AuthCode = "AuthCode-Test" // todo: 鉴权码，配置生成

	c.MsgHeader = msg.MsgHeader
	c.MsgID = 0x8100

	return
}
