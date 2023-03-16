package model

import (
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// 查询终端参数应答
type Msg0104 struct {
	Header             *MsgHeader    `json:"header"`
	AnswerSerialNumber uint16        `json:"answerSerialNumber"` // 应答流水号，对应平台消息的流水号
	AnswerParamCnt     uint8         `json:"answerParamCnt"`     // 应答参数个数
	Parameters         *DeviceParams `json:"parameters"`         // 参数项列表
}

func (m *Msg0104) Decode(packet *PacketData) error {
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	m.AnswerSerialNumber = hex.ReadWord(pkt, &idx)
	m.AnswerParamCnt = hex.ReadByte(pkt, &idx)
	m.Parameters = &DeviceParams{}
	err := m.Parameters.Decode(m.Header.PhoneNumber, m.AnswerParamCnt, pkt[idx:])
	if err != nil {
		log.Error().Err(err).Str("device", m.Header.PhoneNumber).Msg("Fail to decode device params")
		return ErrDecodeMsg
	}
	return nil
}

func (m *Msg0104) Encode() (pkt []byte, err error) {
	pkt = hex.WriteWord(pkt, m.AnswerSerialNumber)
	// AnswerParamCnt will be encode in DeviceParams
	paramBytes, err := m.Parameters.Encode()
	if err != nil {
		log.Error().Err(err).Str("device", m.Header.PhoneNumber).Msg("Fail to encode device params")
		return nil, ErrEncodeMsg
	}
	pkt = hex.WriteBytes(pkt, paramBytes)

	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg0104) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0104) GenOutgoing(incoming JT808Msg) error {
	in, ok := incoming.(*Msg8104)
	if !ok {
		return ErrGenOutgoingMsg
	}
	m.AnswerSerialNumber = in.Header.SerialNumber
	m.Header = in.Header
	m.Header.MsgID = 0x0104

	return nil
}
