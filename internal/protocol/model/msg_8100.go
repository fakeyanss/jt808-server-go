package model

import "encoding/binary"

type ResultCodeType byte

const (
	ResSuccess               ResultCodeType = 0
	ResCarAlreadyRegister    ResultCodeType = 1
	ResCarNotExist           ResultCodeType = 2
	ResDeviceAlreadyRegister ResultCodeType = 3
	ResDeviceNotExist        ResultCodeType = 4
)

// 终端注册应答消息
type Msg8100 struct {
	Header             *MsgHeader     `json:"header"`
	AnswerSerialNumber uint16         `json:"answerSerialNumber"` // 应答流水号，对应平台消息的流水号
	Result             ResultCodeType `json:"result"`             // 结果，0成功，1车辆已被注册，2数据库中无此车辆，3此终端已被注册，4数据库中无此终端
	AuthCode           string         `json:"authCode"`           // 鉴权码
}

func (m *Msg8100) Encode() (pkt []byte, err error) {
	asn := make([]byte, 2)
	binary.BigEndian.PutUint16(asn, m.AnswerSerialNumber)
	pkt = append(pkt, asn...)

	pkt = append(pkt, byte(m.Result))

	pkt = append(pkt, []byte(m.AuthCode)...)

	m.Header.Attr.BodyLength = uint16(len(pkt))

	headerPkt, err := m.Header.Encode()
	if err != nil {
		return nil, err
	}

	pkt = append(headerPkt, pkt...)

	return pkt, nil
}

func (m *Msg8100) Decode(packet *PacketData) error {
	return nil
}

func (m *Msg8100) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg8100) GenOutgoing(incoming JT808Msg) error {
	in := incoming.(*Msg0100)
	m.AnswerSerialNumber = in.Header.SerialNumber
	m.Result = 0
	m.AuthCode = "AuthCode" // 初始值，在后续处理中根据id重写

	m.Header = in.Header
	m.Header.MsgID = 0x8100

	return nil
}
