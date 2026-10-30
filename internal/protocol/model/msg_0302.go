package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg0302 is a terminal question response.
type Msg0302 struct {
	Header       *MsgHeader `json:"header"`
	AnswerSerial uint16     `json:"answerSerial"` // 应答流水号
	AnswerID     uint32     `json:"answerId"`     // 答案ID
}

func (m *Msg0302) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 6 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.AnswerSerial = hex.ReadWord(p.Body, &idx)
	m.AnswerID = hex.ReadDoubleWord(p.Body, &idx)
	return nil
}

func (m *Msg0302) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt := hex.WriteWord(nil, m.AnswerSerial)
	pkt = hex.WriteDoubleWord(pkt, m.AnswerID)
	return writeHeader(m, pkt)
}

func (m *Msg0302) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0302) GenOutgoing(_ JT808Msg) error {
	return nil
}
