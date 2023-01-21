package model

import "encoding/binary"

type JT808Cmd interface {
	Encode() ([]byte, error) // struct -> []byte
}

// 平台通用应答
type Cmd8001 struct {
	MsgHeader
	AnswerSerialNumber uint16 `json:"answerSerialNumber"` // 应答流水号，对应平台消息的流水号
	AnswerMessageID    uint16 `json:"answerMessageId"`    // 应答ID，对应平台消息的ID
	Result             uint8  `json:"result"`             // 结果，0成功/确认，1失败，2消息有误，3不支持
}

func (c *Cmd8001) Encode() ([]byte, error) {
	return nil, nil
}

// 终端注册应答消息
type Cmd8100 struct {
	MsgHeader
	AnswerSerialNumber uint16 `json:"answerSerialNumber"`
	Result             byte   `json:"result"`
	AuthCode           string `json:"authCode"`
}

func (c *Cmd8100) Encode() ([]byte, error) {
	pkt := make([]byte, 0)

	asn := make([]byte, 2)
	binary.BigEndian.PutUint16(asn, c.AnswerSerialNumber)
	pkt = append(pkt, asn...)

	pkt = append(pkt, c.Result)

	pkt = append(pkt, []byte(c.AuthCode)...)

	c.BodyLength = uint16(len(pkt))

	headerPkt, err := c.MsgHeader.Encode()
	if err != nil {
		return nil, err
	}

	pkt = append(headerPkt, pkt...)

	return pkt, nil
}
