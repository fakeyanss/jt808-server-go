package model

import "encoding/binary"

// 终端通用应答
type Msg0001 struct {
	Header             *MsgHeader `json:"header"`
	AnswerSerialNumber uint16     `json:"answerSerialNumber"` // 2位，应答流水号，对应平台消息的流水号，
	AnswerMessageID    uint16     `json:"answerMessageId"`    // 2位，应答ID，对应平台消息的ID
	Result             uint8      `json:"result"`             // 1位，结果，0成功/确认，1失败，2消息有误，3不支持
}

func (m *Msg0001) Decode(packet *PacketData) error {
	m.Header = packet.Header

	pkt := packet.Body
	idx := 0

	m.AnswerSerialNumber = binary.BigEndian.Uint16(pkt[idx : idx+2])
	idx += 2

	m.AnswerMessageID = binary.BigEndian.Uint16(pkt[idx : idx+2])
	idx += 2

	m.Result = pkt[idx]
	idx++

	return nil
}

func (m *Msg0001) Encode() (pkt []byte, err error) {
	asn := make([]byte, 2)
	binary.BigEndian.PutUint16(asn, m.AnswerSerialNumber)
	pkt = append(pkt, asn...)

	amid := make([]byte, 2)
	binary.BigEndian.PutUint16(amid, m.AnswerMessageID)
	pkt = append(pkt, amid...)

	pkt = append(pkt, byte(m.Result))

	headerPkt, err := m.Header.Encode()
	if err != nil {
		return nil, err
	}
	pkt = append(headerPkt, pkt...)
	return pkt, nil
}

func (m *Msg0001) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0001) GenOutgoing(incoming JT808Msg) error {
	return nil
}
