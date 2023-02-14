package model

import (
	"github.com/pkg/errors"
)

var ErrDecodeMsg = errors.New("Fail to decode Msg")

type JT808Msg interface {
	Decode(*PacketData) error        // Packet -> JT808Msg
	Encode() (pkt []byte, err error) // JT808Msg -> Packet
	GetHeader() *MsgHeader
	GenOutgoing(incoming JT808Msg) error
}
