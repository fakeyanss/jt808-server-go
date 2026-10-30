package model

// MsgRawBody is used for JT808 bodies whose payload is defined by an external
// industry standard (such as GB/T 19056) or an optional vendor data block.
type MsgRawBody struct {
	Header *MsgHeader `json:"header"`
	Body   []byte     `json:"body"`
}

func (m *MsgRawBody) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	m.Body = append([]byte(nil), packet.Body...)
	return nil
}
func (m *MsgRawBody) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	return writeHeader(m, append([]byte(nil), m.Body...))
}
func (m *MsgRawBody) GetHeader() *MsgHeader        { return m.Header }
func (m *MsgRawBody) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
