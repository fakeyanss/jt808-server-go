package model

// Msg8204 is an empty-body server-initiated link detection request.
type Msg8204 struct {
	Header *MsgHeader `json:"header"`
}

func (m *Msg8204) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) != 0 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	return nil
}
func (m *Msg8204) Encode() ([]byte, error)      { return writeHeader(m, nil) }
func (m *Msg8204) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8204) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
