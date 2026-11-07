package model

import (
	"github.com/pkg/errors"
)

const (
	MsgID0002 = 0x0002
	MsgID0100 = 0x0100
	MsgID0200 = 0x0200
	MsgID8004 = 0x8004
)

var (
	ErrDecodeMsg      = errors.New("Fail to decode msg")
	ErrEncodeMsg      = errors.New("Fail to encode msg")
	ErrGenOutgoingMsg = errors.New("Fail to generate outgoing msg")
)

type JT808Msg interface {
	Decode(*PacketData) error            // Packet -> JT808Msg
	Encode() (pkt []byte, err error)     // JT808Msg -> Packet
	GetHeader() *MsgHeader               // 获取Header
	GenOutgoing(incoming JT808Msg) error // 根据incoming消息生成outgoing消息
}

// AnswerSerial 提取应答消息携带的被应答流水号。
// 返回 false 表示该消息不是应答，或协议体中没有应答流水号字段：
// 0x0608/0x0702/0x8800 只能由调用方按消息类型兜底处理。
func AnswerSerial(msg JT808Msg) (uint16, bool) {
	switch m := msg.(type) {
	case *Msg0001:
		return m.AnswerSerialNumber, true
	case *Msg8001:
		return m.AnswerSerialNumber, true
	case *Msg0104:
		return m.AnswerSerialNumber, true
	case *Msg0201:
		return m.AnswerSerialNumber, true
	case *Msg1205:
		return m.AnswerSerialNumber, true
	case *Msg0700:
		return m.AnswerSerial, true
	case *Msg0802:
		return m.AnswerSerial, true
	case *Msg0805:
		return m.AnswerSerial, true
	}
	return 0, false
}

func writeHeader(m JT808Msg, pkt []byte) ([]byte, error) {
	m.GetHeader().Attr.BodyLength = uint16(len(pkt))
	headerPkt, err := m.GetHeader().Encode()
	if err != nil {
		return nil, err
	}
	return append(headerPkt, pkt...), nil
}

// cloneReplyHeader keeps encoding a response from changing the decoded
// incoming message header or its body attributes.
func cloneReplyHeader(in *MsgHeader, messageID uint16) *MsgHeader {
	if in == nil {
		return nil
	}
	out := *in
	out.MsgID = messageID
	if in.Attr != nil {
		attr := *in.Attr
		out.Attr = &attr
	}
	if in.Frag != nil {
		frag := *in.Frag
		out.Frag = &frag
	}
	return &out
}
