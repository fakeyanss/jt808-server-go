package model

import (
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// 苏标 T/JSATL 12-2017 4.6.5 文件上传完成消息（0x1212）
//
// 终端完成一个文件的数据发送后上报，平台以 0x9212 应答。
// 消息体格式与 0x1211（表 4-25）一致。
type Msg1212 struct {
	Header   *MsgHeader `json:"header"`
	FileName string     `json:"fileName"`
	FileType uint8      `json:"fileType"`
	FileSize uint32     `json:"fileSize"`
}

func (m *Msg1212) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	if len(pkt) < 1 {
		return ErrDecodeMsg
	}
	nameLen := int(hex.ReadByte(pkt, &idx))
	if nameLen == 0 || idx+nameLen+5 > len(pkt) {
		return ErrDecodeMsg
	}
	m.FileName = hex.ReadString(pkt, &idx, nameLen)
	m.FileType = hex.ReadByte(pkt, &idx)
	m.FileSize = hex.ReadDoubleWord(pkt, &idx)
	return nil
}

func (m *Msg1212) Encode() (pkt []byte, err error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	name := []byte(m.FileName)
	if len(name) == 0 || len(name) > 255 {
		return nil, ErrEncodeMsg
	}
	pkt = hex.WriteByte(pkt, uint8(len(name)))
	pkt = hex.WriteBytes(pkt, name)
	pkt = hex.WriteByte(pkt, m.FileType)
	pkt = hex.WriteDoubleWord(pkt, m.FileSize)
	return writeHeader(m, pkt)
}

func (m *Msg1212) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg1212) GenOutgoing(_ JT808Msg) error {
	return ErrGenOutgoingMsg
}
