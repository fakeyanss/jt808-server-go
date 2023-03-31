package model

import (
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// 设置终端参数
type Msg8103 struct {
	Header     *MsgHeader    `json:"header"`
	ParamCnt   uint8         `json:"paramCnt"`   // 参数个数
	Parameters *DeviceParams `json:"parameters"` // 参数项列表
}

// client接收到消息，解析为结构体
func (m *Msg8103) Decode(packet *PacketData) error {
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	m.ParamCnt = hex.ReadByte(pkt, &idx)
	m.Parameters = &DeviceParams{}
	err := m.Parameters.Decode(m.Header.PhoneNumber, m.ParamCnt, pkt[idx:])
	if err != nil {
		log.Error().Err(err).Str("device", m.Header.PhoneNumber).Msg("Fail to decode device params")
		return ErrDecodeMsg
	}
	return nil
}

// server端发送8103消息，编码为字节数组
func (m *Msg8103) Encode() (pkt []byte, err error) {
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

func (m *Msg8103) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg8103) GenOutgoing(_ JT808Msg) error {
	// will not use
	return nil
}
