package model

import (
	"net"
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

	MsgCtxKey struct{}

	CmdCtxKey struct{}

	PacketEncodeCtxKey struct{}
)

type Session struct {
	ID   string
	Conn net.Conn
}

func (s *Session) GetTransProto() TransportProtocol {
	if s.Conn != nil {
		return TCPProto
	}
	return UDPProto
}

// 定义PacketData Data
type PacketData struct {
	Header     *MsgHeader // 消息头
	Body       []byte     // 消息体
	VerifyCode byte       // 校验码
}

// 定义消息处理结果数据
type ProcessData struct {
	Msg JT808Msg
	Cmd JT808Cmd
}
