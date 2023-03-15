package model

import (
	"math"
	"net"
	"sync/atomic"
)

const (
	TCPProto TransportProtocol = "TCP"
	UDPProto TransportProtocol = "UDP"
)

type TransportProtocol string

type (
	SessionCtxKey struct{} // 定义全局session context key

	FrameCtxKey struct{}

	PacketDecodeCtxKey struct{}

	ProcessDataCtxKey struct{}

	IncomingMsgCtxKey struct{}

	OutgoingMsgCtxKey struct{}

	PacketEncodeCtxKey struct{}
)

type Session struct {
	ID           string // remote addr
	Conn         net.Conn
	serialNumber uint32
}

func (s *Session) GetTransProto() TransportProtocol {
	if s.Conn != nil {
		return TCPProto
	}
	return UDPProto
}

func (s *Session) GetNextSerialNum() uint16 {
	next := atomic.AddUint32(&s.serialNumber, 1)
	if next <= math.MaxUint16 {
		return uint16(next)
	}
	atomic.StoreUint32(&s.serialNumber, 0)
	return uint16(s.serialNumber)
}

// 定义Packet Data结构
type PacketData struct {
	Header     *MsgHeader // 消息头
	Body       []byte     // 消息体
	VerifyCode byte       // 校验码
}

// 定义消息处理结果数据
type ProcessData struct {
	Incoming JT808Msg // 收到的消息
	Outgoing JT808Msg // 发出的消息, 无需回复时可为nil
}
