package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg860Delete encodes deletion of circular/rectangular/polygonal areas or
// routes (0x8601, 0x8603, 0x8605, 0x8607). A zero ID count means delete all.
type Msg860Delete struct {
	Header *MsgHeader `json:"header"`
	IDs    []uint32   `json:"ids"`
}

func (m *Msg860Delete) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) < 1 {
		return ErrDecodeMsg
	}
	count := int(packet.Body[0])
	if count > 125 || len(packet.Body) != 1+count*4 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	m.IDs = make([]uint32, 0, count)
	idx := 1
	for i := 0; i < count; i++ {
		m.IDs = append(m.IDs, hex.ReadDoubleWord(packet.Body, &idx))
	}
	return nil
}

func (m *Msg860Delete) Encode() ([]byte, error) {
	if m.Header == nil || len(m.IDs) > 125 {
		return nil, ErrEncodeMsg
	}
	body := []byte{byte(len(m.IDs))}
	for _, id := range m.IDs {
		body = hex.WriteDoubleWord(body, id)
	}
	return writeHeader(m, body)
}

func (m *Msg860Delete) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg860Delete) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
