package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg8804 starts or stops audio recording.
type Msg8804 struct {
	Header     *MsgHeader `json:"header"`
	Command    uint8      `json:"command"`
	Duration   uint16     `json:"duration"`
	Save       uint8      `json:"save"`
	SampleRate uint8      `json:"sampleRate"`
}

func (m *Msg8804) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) != 5 || p.Body[0] > 1 || p.Body[3] > 1 || p.Body[4] > 3 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	i := 0
	m.Command = hex.ReadByte(p.Body, &i)
	m.Duration = hex.ReadWord(p.Body, &i)
	m.Save = hex.ReadByte(p.Body, &i)
	m.SampleRate = hex.ReadByte(p.Body, &i)
	return nil
}
func (m *Msg8804) Encode() ([]byte, error) {
	if m.Header == nil || m.Command > 1 || m.Save > 1 || m.SampleRate > 3 {
		return nil, ErrEncodeMsg
	}
	b := []byte{m.Command}
	b = hex.WriteWord(b, m.Duration)
	b = append(b, m.Save, m.SampleRate)
	return writeHeader(m, b)
}
func (m *Msg8804) GetHeader() *MsgHeader      { return m.Header }
func (m *Msg8804) GenOutgoing(JT808Msg) error { return ErrGenOutgoingMsg }
