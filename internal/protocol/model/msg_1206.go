package model

import (
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// JT/T 1078-2016 文件上传完成通知（0x1206）
//
// 终端完成 0x9206 文件上传后向平台通知结果。
// 注：任务描述把 0x1206 称为"文件上传完成通知应答"，与两份参考实现
// （SmallChi JT1078 扩展、go-jt808）的消息表不一致，此处按双源一致定义实现。
type Msg1206 struct {
	Header             *MsgHeader `json:"header"`
	AnswerSerialNumber uint16     `json:"answerSerialNumber"` // 流水号，对应平台文件上传指令的流水号
	Result             uint8      `json:"result"`             // 结果。0 成功；1 失败
}

func (m *Msg1206) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) != 3 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	m.AnswerSerialNumber = hex.ReadWord(pkt, &idx)
	m.Result = hex.ReadByte(pkt, &idx)
	return nil
}

func (m *Msg1206) Encode() (pkt []byte, err error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt = hex.WriteWord(pkt, m.AnswerSerialNumber)
	pkt = hex.WriteByte(pkt, m.Result)
	return writeHeader(m, pkt)
}

func (m *Msg1206) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg1206) GenOutgoing(_ JT808Msg) error {
	return nil
}
