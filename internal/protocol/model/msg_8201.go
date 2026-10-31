package model

// Msg8201 requests the terminal's current location.
type Msg8201 struct {
	Header *MsgHeader `json:"header"`
}

func (m *Msg8201) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) != 0 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	return nil
}
func (m *Msg8201) Encode() ([]byte, error)      { return writeHeader(m, nil) }
func (m *Msg8201) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8201) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
