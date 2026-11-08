package model

import (
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// JT/T 1078-2016 实时音视频传输请求（0x9101）
//
// 平台下发该消息通知终端的音视频数据推流目标地址。
// 字段定义与 SmallChi JT1078 扩展、go-jt808 实现一致：
// IP 地址长度 1 字节、IP 地址变长、TCP 端口 2 字节、UDP 端口 2 字节，
// 逻辑通道号、数据类型、码流类型各 1 字节。
type Msg9101 struct {
	Header         *MsgHeader `json:"header"`
	ServerIPLength uint8      `json:"serverIpLength"` // 视频服务器 IP 地址长度
	ServerIP       string     `json:"serverIp"`       // 视频服务器 IP 地址
	TCPPort        uint16     `json:"tcpPort"`        // 视频服务器 TCP 端口号，0 表示不使用 TCP
	UDPPort        uint16     `json:"udpPort"`        // 视频服务器 UDP 端口号，0 表示不使用 UDP
	LogicChannelID uint8      `json:"logicChannelId"` // 逻辑通道号
	DataType       uint8      `json:"dataType"`       // 数据类型。0 音视频；1 视频；2 双向对讲；3 监听；4 中心广播；5 透传
	StreamType     uint8      `json:"streamType"`     // 码流类型。0 主码流；1 子码流
}

func (m *Msg9101) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) < 1 {
		return ErrDecodeMsg
	}
	pkt, idx := packet.Body, 0
	n := int(hex.ReadByte(pkt, &idx))
	if idx+n+7 != len(pkt) {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	m.ServerIPLength = uint8(n)
	m.ServerIP = hex.ReadString(pkt, &idx, n)
	m.TCPPort = hex.ReadWord(pkt, &idx)
	m.UDPPort = hex.ReadWord(pkt, &idx)
	m.LogicChannelID = hex.ReadByte(pkt, &idx)
	m.DataType = hex.ReadByte(pkt, &idx)
	m.StreamType = hex.ReadByte(pkt, &idx)
	return nil
}

func (m *Msg9101) Encode() (pkt []byte, err error) {
	if m.Header == nil || len(m.ServerIP) > 255 {
		return nil, ErrEncodeMsg
	}
	m.ServerIPLength = uint8(len(m.ServerIP))
	pkt = hex.WriteByte(pkt, m.ServerIPLength)
	pkt = hex.WriteString(pkt, m.ServerIP)
	pkt = hex.WriteWord(pkt, m.TCPPort)
	pkt = hex.WriteWord(pkt, m.UDPPort)
	pkt = hex.WriteByte(pkt, m.LogicChannelID)
	pkt = hex.WriteByte(pkt, m.DataType)
	pkt = hex.WriteByte(pkt, m.StreamType)
	return writeHeader(m, pkt)
}

func (m *Msg9101) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg9101) GenOutgoing(_ JT808Msg) error {
	return nil
}
