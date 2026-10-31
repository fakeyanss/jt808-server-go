package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

type Msg0700 struct {
	Header       *MsgHeader `json:"header"`
	AnswerSerial uint16     `json:"answerSerial"`
	Command      uint8      `json:"command"`
	Data         []byte     `json:"data"`
	DataBlock    any        `json:"dataBlock,omitempty"` // typed response block, nil if unsupported
}

func (m *Msg0700) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 3 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	i := 0
	m.AnswerSerial = hex.ReadWord(p.Body, &i)
	m.Command = hex.ReadByte(p.Body, &i)
	m.Data = append([]byte(nil), p.Body[i:]...)
	m.DataBlock = decodeRecorderResponseBlock(m.Command, m.Data)
	return nil
}
func (m *Msg0700) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	data, err := encodeRecorderBlock(m.DataBlock, m.Data)
	if err != nil {
		return nil, err
	}
	b := hex.WriteWord(nil, m.AnswerSerial)
	b = append(b, m.Command)
	b = append(b, data...)
	return writeHeader(m, b)
}
func (m *Msg0700) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0700) GenOutgoing(in JT808Msg) error {
	cmd, ok := in.(*MsgRecordCommand)
	if !ok {
		return ErrGenOutgoingMsg
	}
	m.Header = cloneReplyHeader(cmd.Header, 0x0700)
	m.AnswerSerial = cmd.Header.SerialNumber
	m.Command = cmd.Command
	return nil
}
