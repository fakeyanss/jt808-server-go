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
	case 0x0100:
		msg = &model.Msg0100{}
	default:
		err = fmt.Errorf("unexpected msgID %v", msgID)
	}
	return msg, err
}

func (h *JT808MsgHandler) ProcessMsg(msg model.JT808Msg) (cmd model.JT808Cmd, err error) {
	switch t := msg.(type) {
	case *model.Msg0100:
		m, _ := msg.(*model.Msg0100)
		cmd, err = h.genCmd8100(*m)
	case *model.Msg0001:
		// m, _ := msg.(*model.Msg0001)
		// cmd, err := h.
	default:
		err = fmt.Errorf("unexpected type %T", t)
	}
	return cmd, err
}

func (h *JT808MsgHandler) genCmd8100(msg model.Msg0100) (cmd model.JT808Cmd, err error) {
	c := cmd.(*model.Cmd8100)
	c.AnswerSerialNumber = msg.SerialNumber
	c.Result = 0
	c.AuthCode = "AuthCode-Test" // todo: 鉴权码，配置生成

	c.MsgHeader = msg.MsgHeader
	c.MsgID = 0x8100

	return c, err
}

// func (h *JT808MsgHandler) genCmd
