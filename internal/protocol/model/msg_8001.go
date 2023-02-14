package model

import "encoding/binary"

type ResultCode uint8

const (
	ResultSuccess      ResultCode = 0
	ResultFail         ResultCode = 1
	ResultErrMsg       ResultCode = 2
	ResultNotSupported ResultCode = 3
)

// 平台通用应答
type Msg8001 struct {
	Header             *MsgHeader `json:"header"`
	AnswerSerialNumber uint16     `json:"answerSerialNumber"` // 应答流水号，对应平台消息的流水号
	AnswerMessageID    uint16     `json:"answerMessageId"`    // 应答ID，对应平台消息的ID
	Result             ResultCode `json:"result"`             // 结果，0成功/确认，1失败，2消息有误，3不支持
}

func (m *Msg8001) Encode() (pkt []byte, err error) {
	asn := make([]byte, 2)
	binary.BigEndian.PutUint16(asn, m.AnswerSerialNumber)
	pkt = append(pkt, asn...)

	amid := make([]byte, 2)
	binary.BigEndian.PutUint16(amid, m.AnswerMessageID)
	pkt = append(pkt, amid...)

	pkt = append(pkt, byte(m.Result))

	m.Header.Attr.BodyLength = uint16(len(pkt))

	headerPkt, err := m.Header.Encode()
	if err != nil {
		return nil, err
	}

	pkt = append(headerPkt, pkt...)

	return pkt, nil
}

func (m *Msg8001) Decode(packet *PacketData) error {
	return nil
}

func (m *Msg8001) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg8001) GenOutgoing(incoming JT808Msg) error {
	header := incoming.GetHeader()
	m.AnswerSerialNumber = header.SerialNumber
	m.AnswerMessageID = header.MsgID
	m.Result = 0

	m.Header = header
	m.Header.MsgID = 0x8001

	return nil
}
