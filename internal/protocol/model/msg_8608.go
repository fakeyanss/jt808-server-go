package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg8608 requests a specific type of area/route data; an empty ID list asks
// for all data of that type.
type Msg8608 struct {
	Header    *MsgHeader `json:"header"`
	QueryType uint8      `json:"queryType"`
	IDs       []uint32   `json:"ids"`
}

func (m *Msg8608) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) < 5 {
		return ErrDecodeMsg
	}
	count := int(packet.Body[1])<<24 | int(packet.Body[2])<<16 | int(packet.Body[3])<<8 | int(packet.Body[4])
	if packet.Body[0] < 1 || packet.Body[0] > 4 || count > 125 || len(packet.Body) != 5+count*4 {
		return ErrDecodeMsg
	}
	m.Header, m.QueryType = packet.Header, packet.Body[0]
	m.IDs = make([]uint32, 0, count)
	idx := 5
	for i := 0; i < count; i++ {
		m.IDs = append(m.IDs, hex.ReadDoubleWord(packet.Body, &idx))
	}
	return nil
}
func (m *Msg8608) Encode() ([]byte, error) {
	if m.Header == nil || m.QueryType < 1 || m.QueryType > 4 || len(m.IDs) > 125 {
		return nil, ErrEncodeMsg
	}
	body := []byte{m.QueryType}
	body = hex.WriteDoubleWord(body, uint32(len(m.IDs)))
	for _, id := range m.IDs {
		body = hex.WriteDoubleWord(body, id)
	}
	return writeHeader(m, body)
}
func (m *Msg8608) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8608) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
