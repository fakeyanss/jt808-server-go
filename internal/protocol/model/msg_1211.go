package model

import (
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// 苏标 T/JSATL 12-2017 4.6.3 文件信息上传（0x1211）
//
// 终端在报警附件信息（0x1210）被应答后，逐个文件上报文件名称、类型与大小。
type Msg1211 struct {
	Header   *MsgHeader `json:"header"`
	FileName string     `json:"fileName"`
	FileType uint8      `json:"fileType"` // 0x00 图片；0x01 音频；0x02 视频；0x03 文本；0x04 其它
	FileSize uint32     `json:"fileSize"`
}

func (m *Msg1211) Decode(packet *PacketData) error {
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

func (m *Msg1211) Encode() (pkt []byte, err error) {
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

func (m *Msg1211) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg1211) GenOutgoing(_ JT808Msg) error {
	return ErrGenOutgoingMsg
}
