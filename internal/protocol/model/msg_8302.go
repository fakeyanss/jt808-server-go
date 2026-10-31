package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// QuestionCandidate is one selectable answer in a 0x8302 question.
type QuestionCandidate struct {
	ID      uint32 `json:"id"`      // 答案ID
	Content string `json:"content"` // 答案内容 GBK
}

// Msg8302 is a platform question-issue command.
type Msg8302 struct {
	Header     *MsgHeader          `json:"header"`
	Flag       uint8               `json:"flag"`       // 标志 bit0:紧急 bit1:TTS
	Question   string              `json:"question"`   // 问题内容 GBK
	Candidates []QuestionCandidate `json:"candidates"` // 候选答案
}

func (m *Msg8302) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 3 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.Flag = hex.ReadByte(p.Body, &idx)
	qLen := int(hex.ReadByte(p.Body, &idx))
	if idx+qLen > len(p.Body) {
		return ErrDecodeMsg
	}
	m.Question = hex.ReadGBK(p.Body, &idx, qLen)
	if idx >= len(p.Body) {
		return nil
	}
	count := int(hex.ReadByte(p.Body, &idx))
	m.Candidates = nil
	for n := 0; n < count; n++ {
		if idx+5 > len(p.Body) {
			return ErrDecodeMsg
		}
		c := QuestionCandidate{ID: hex.ReadDoubleWord(p.Body, &idx)}
		contentLen := int(hex.ReadByte(p.Body, &idx))
		if idx+contentLen > len(p.Body) {
			return ErrDecodeMsg
		}
		c.Content = hex.ReadGBK(p.Body, &idx, contentLen)
		m.Candidates = append(m.Candidates, c)
	}
	return nil
}

func (m *Msg8302) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	gbk := hex.WriteGBK(nil, m.Question)
	pkt := []byte{m.Flag, uint8(len(gbk))}
	pkt = append(pkt, gbk...)
	pkt = append(pkt, uint8(len(m.Candidates)))
	for _, c := range m.Candidates {
		cgbk := hex.WriteGBK(nil, c.Content)
		pkt = hex.WriteDoubleWord(pkt, c.ID)
		pkt = append(pkt, uint8(len(cgbk)))
		pkt = append(pkt, cgbk...)
	}
	return writeHeader(m, pkt)
}

func (m *Msg8302) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8302) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
