package model

// Msg0301 is a terminal event report.
type Msg0301 struct {
	Header  *MsgHeader `json:"header"`
	EventID uint8      `json:"eventId"` // 事件ID
}

func (m *Msg0301) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 1 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	m.EventID = p.Body[0]
	return nil
}

func (m *Msg0301) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	return writeHeader(m, []byte{m.EventID})
}

func (m *Msg0301) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0301) GenOutgoing(_ JT808Msg) error {
	return nil
}
