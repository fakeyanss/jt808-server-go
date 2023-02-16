package model

import (
	"github.com/pkg/errors"
)

var (
	ErrDecodeMsg      = errors.New("Fail to decode msg")
	ErrGenOutgoingMsg = errors.New("Fail to generate outgoing msg")
)

type JT808Msg interface {
	Decode(*PacketData) error            // Packet -> JT808Msg
	Encode() (pkt []byte, err error)     // JT808Msg -> Packet
	GetHeader() *MsgHeader               // 获取Header
	GenOutgoing(incoming JT808Msg) error // 根据incoming消息生成outgoing消息
}

func writeHeader(m JT808Msg, pkt []byte) ([]byte, error) {
	m.GetHeader().Attr.BodyLength = uint16(len(pkt))
	headerPkt, err := m.GetHeader().Encode()
	if err != nil {
		return nil, err
	}
	return append(headerPkt, pkt...), nil
}
