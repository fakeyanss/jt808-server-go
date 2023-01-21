package protocol

import "fmt"

type MsgHandler interface {
	Handle(JT808Msg) (JT808Cmd, error)
}

type JT808MsgHandler struct {
}

func NewJT808MsgHandler() *JT808MsgHandler {
	return &JT808MsgHandler{}
}

func (h *JT808MsgHandler) Handle(msg JT808Msg) (JT808Cmd, error) {
	var cmd JT808Cmd
	var err error
	switch t := msg.(type) {
	case *Msg0100:
		m, _ := msg.(*Msg0100)
		cmd, err = h.genCmd8100(*m)
	default:
		err = fmt.Errorf("unexpected type %T", t)
	}
	return cmd, err
}

func (h *JT808MsgHandler) genCmd8100(msg Msg0100) (JT808Cmd, error) {
	cmd := &Cmd8100{}
	var err error

	cmd.AnswerSerialNumber = msg.SerialNumber
	cmd.Result = 0
	cmd.AuthCode = "123"

	cmd.JT808MsgHeader = msg.JT808MsgHeader

	return cmd, err
}
