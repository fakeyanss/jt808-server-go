package model

import (
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// 苏标 T/JSATL 12-2017 4.6.6 文件上传完成消息应答（0x9212）
//
// 补传列表按表 4-29 使用“字节偏移量 + 字节长度”，不是块序号。
// 终端按列表补传数据后重新上报 0x1212，直至平台应答完成。
type Msg9212 struct {
	Header            *MsgHeader         `json:"header"`
	FileName          string             `json:"fileName"`
	FileType          uint8              `json:"fileType"`
	Result            uint8              `json:"result"` // 0x00 完成；0x01 需要补传
	RetransmitCount   uint8              `json:"retransmitCount"`
	RetransmitPackets []RetransmitPacket `json:"retransmitPackets"`
}

// RetransmitPacket 是需要补传的数据范围（表 4-29）。
// Offset 与 Length 均为字节单位。
type RetransmitPacket struct {
	Offset uint32 `json:"offset"`
	Length uint32 `json:"length"`
}

func (m *Msg9212) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	if len(pkt) < 1 {
		return ErrDecodeMsg
	}
	nameLen := int(hex.ReadByte(pkt, &idx))
	// 文件名 + 文件类型 + 上传结果 + 补传数量
	if nameLen == 0 || idx+nameLen+3 > len(pkt) {
		return ErrDecodeMsg
	}
	m.FileName = hex.ReadString(pkt, &idx, nameLen)
	m.FileType = hex.ReadByte(pkt, &idx)
	m.Result = hex.ReadByte(pkt, &idx)
	m.RetransmitCount = hex.ReadByte(pkt, &idx)
	m.RetransmitPackets = nil
	for n := uint8(0); n < m.RetransmitCount; n++ {
		if idx+8 > len(pkt) {
			return ErrDecodeMsg
		}
		m.RetransmitPackets = append(m.RetransmitPackets, RetransmitPacket{
			Offset: hex.ReadDoubleWord(pkt, &idx),
			Length: hex.ReadDoubleWord(pkt, &idx),
		})
	}
	return nil
}

func (m *Msg9212) Encode() (pkt []byte, err error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	name := []byte(m.FileName)
	if len(name) == 0 || len(name) > 255 {
		return nil, ErrEncodeMsg
	}
	if len(m.RetransmitPackets) > 255 {
		return nil, ErrEncodeMsg
	}
	pkt = hex.WriteByte(pkt, uint8(len(name)))
	pkt = hex.WriteBytes(pkt, name)
	pkt = hex.WriteByte(pkt, m.FileType)
	pkt = hex.WriteByte(pkt, m.Result)
	pkt = hex.WriteByte(pkt, uint8(len(m.RetransmitPackets)))
	for _, packet := range m.RetransmitPackets {
		pkt = hex.WriteDoubleWord(pkt, packet.Offset)
		pkt = hex.WriteDoubleWord(pkt, packet.Length)
	}
	return writeHeader(m, pkt)
}

func (m *Msg9212) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg9212) GenOutgoing(incoming JT808Msg) error {
	in, ok := incoming.(*Msg1212)
	if !ok {
		return ErrGenOutgoingMsg
	}
	m.FileName = in.FileName
	m.FileType = in.FileType
	m.Result = 0
	m.RetransmitCount = 0
	m.RetransmitPackets = nil
	m.Header = cloneReplyHeader(in.Header, 0x9212)
	return nil
}
