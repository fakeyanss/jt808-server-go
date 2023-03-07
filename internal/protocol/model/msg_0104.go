package model

import (
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/rs/zerolog/log"
)

// 查询终端参数应答
type Msg0104 struct {
	Header             *MsgHeader  `json:"header"`
	AnswerSerialNumber uint16      `json:"answerSerialNumber"` // 应答流水号，对应平台消息的流水号
	AnswerArgsCnt      uint8       `json:"answerArgsCnt"`      // 应答参数个数
	Args               *DeviceArgs `json:"args"`               // 参数项列表
}

func (m *Msg0104) Decode(packet *PacketData) error {
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	m.AnswerSerialNumber = hex.ReadWord(pkt, &idx)
	m.AnswerArgsCnt = hex.ReadByte(pkt, &idx)
	m.Args = &DeviceArgs{}
	err := m.Args.Decode(m.AnswerArgsCnt, pkt)
	if err != nil {
		log.Error().Err(err).Str("device", m.Header.PhoneNumber).Msg("Fail to decode device args")
		return ErrDecodeMsg
	}
	return nil
}

func (m *Msg0104) Encode() (pkt []byte, err error) {
	pkt = hex.WriteWord(pkt, m.AnswerSerialNumber)
	pkt = hex.WriteByte(pkt, m.AnswerArgsCnt)
	argBytes, err := m.Args.Encode()
	if err != nil {
		log.Error().Err(err).Str("device", m.Header.PhoneNumber).Msg("Fail to encode device args")
		return nil, ErrEncodeMsg
	}
	pkt = hex.WriteBytes(pkt, argBytes)

	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg0104) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0104) GenOutgoing(incoming JT808Msg) error {
	return nil
}
