package model

// Msg8702 asks the terminal to report its current driver identity; its body is empty.
type Msg8702 struct {
	Header *MsgHeader `json:"header"`
}

func (m *Msg8702) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) != 0 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	return nil
}
func (m *Msg8702) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	return writeHeader(m, nil)
}
func (m *Msg8702) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8702) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
