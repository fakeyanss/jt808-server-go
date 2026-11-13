package model

import (
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// JT/T 1078-2016 文件上传控制（0x9207）
//
// 平台暂停、继续或取消某个已下发的文件上传任务。
// 报文体固定 3 字节：应答流水号（对应 0x9206 报文头流水号）+ 控制类型。
type Msg9207 struct {
	Header             *MsgHeader `json:"header"`
	AnswerSerialNumber uint16     `json:"answerSerialNumber"`
	// Control 0 暂停；1 继续；2 取消。
	Control uint8 `json:"control"`
}

func (m *Msg9207) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) != 3 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	m.AnswerSerialNumber = hex.ReadWord(pkt, &idx)
	m.Control = hex.ReadByte(pkt, &idx)
	return nil
}

func (m *Msg9207) Encode() (pkt []byte, err error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt = hex.WriteWord(pkt, m.AnswerSerialNumber)
	pkt = hex.WriteByte(pkt, m.Control)
	return writeHeader(m, pkt)
}

func (m *Msg9207) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg9207) GenOutgoing(_ JT808Msg) error {
	return nil
}
