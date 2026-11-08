package model

import (
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// 苏标 T/JSATL 12-2017 4.5 报警附件上传指令（0x9208）
//
// 平台收到带附件的报警后下发本指令，终端据此连接附件服务器并上传附件。
type Msg9208 struct {
	Header    *MsgHeader `json:"header"`
	IP        string     `json:"ip"`        // 附件服务器 IP 地址
	TCPPort   uint16     `json:"tcpPort"`   // 附件服务器 TCP 端口
	UDPPort   uint16     `json:"udpPort"`   // 附件服务器 UDP 端口
	AlarmSign []byte     `json:"alarmSign"` // 报警标识号，16字节
	AlarmNo   string     `json:"alarmNo"`   // 报警编号，32字节
	Reserved  []byte     `json:"reserved"`  // 预留，16字节
}

func (m *Msg9208) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	if len(pkt) < 1 {
		return ErrDecodeMsg
	}
	ipLen := int(hex.ReadByte(pkt, &idx))
	// IP + TCP 端口 + UDP 端口 + 报警标识号 + 报警编号 + 预留
	if ipLen == 0 || idx+ipLen+2+2+16+32+16 > len(pkt) {
		return ErrDecodeMsg
	}
	m.IP = hex.ReadString(pkt, &idx, ipLen)
	m.TCPPort = hex.ReadWord(pkt, &idx)
	m.UDPPort = hex.ReadWord(pkt, &idx)
	m.AlarmSign = append([]byte(nil), hex.ReadBytes(pkt, &idx, 16)...)
	m.AlarmNo = trimNUL(hex.ReadString(pkt, &idx, 32))
	m.Reserved = append([]byte(nil), hex.ReadBytes(pkt, &idx, 16)...)
	return nil
}

func (m *Msg9208) Encode() (pkt []byte, err error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	ip := []byte(m.IP)
	if len(ip) == 0 || len(ip) > 255 {
		return nil, ErrEncodeMsg
	}
	pkt = hex.WriteByte(pkt, uint8(len(ip)))
	pkt = hex.WriteBytes(pkt, ip)
	pkt = hex.WriteWord(pkt, m.TCPPort)
	pkt = hex.WriteWord(pkt, m.UDPPort)
	pkt = hex.WriteBytes(pkt, padOrTrim(m.AlarmSign, 16))
	pkt = hex.WriteBytes(pkt, padOrTrim([]byte(m.AlarmNo), 32))
	pkt = hex.WriteBytes(pkt, padOrTrim(m.Reserved, 16))
	return writeHeader(m, pkt)
}

func (m *Msg9208) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg9208) GenOutgoing(_ JT808Msg) error {
	return nil
}
