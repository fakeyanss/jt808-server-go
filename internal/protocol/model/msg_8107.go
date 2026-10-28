package model

// Msg8107 is an empty-body query for terminal properties.
type Msg8107 struct {
	Header *MsgHeader `json:"header"`
}

func (m *Msg8107) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) != 0 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	return nil
}

func (m *Msg8107) Encode() ([]byte, error) { return writeHeader(m, nil) }

func (m *Msg8107) GetHeader() *MsgHeader { return m.Header }

func (m *Msg8107) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
