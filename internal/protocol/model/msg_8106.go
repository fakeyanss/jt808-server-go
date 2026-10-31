package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg8106 queries a selected list of terminal parameter IDs.
type Msg8106 struct {
	Header     *MsgHeader `json:"header"`
	ParamCount uint8      `json:"paramCount"`
	ParamIDs   []uint32   `json:"paramIds"`
}

func (m *Msg8106) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) < 1 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	idx := 0
	m.ParamCount = hex.ReadByte(packet.Body, &idx)
	if len(packet.Body)-idx != int(m.ParamCount)*4 {
		return ErrDecodeMsg
	}
	m.ParamIDs = make([]uint32, 0, m.ParamCount)
	for i := 0; i < int(m.ParamCount); i++ {
		m.ParamIDs = append(m.ParamIDs, hex.ReadDoubleWord(packet.Body, &idx))
	}
	return nil
}

func (m *Msg8106) Encode() ([]byte, error) {
	if len(m.ParamIDs) > 255 {
		return nil, ErrEncodeMsg
	}
	pkt := hex.WriteByte(nil, uint8(len(m.ParamIDs)))
	for _, id := range m.ParamIDs {
		pkt = hex.WriteDoubleWord(pkt, id)
	}
	return writeHeader(m, pkt)
}

func (m *Msg8106) GetHeader() *MsgHeader { return m.Header }

func (m *Msg8106) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
