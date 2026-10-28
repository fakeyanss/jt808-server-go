package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// MsgRegionRouteSet preserves the variable-length area/route item payloads.
// The item layouts vary with area attributes and protocol version, so the
// validated standard message body is carried as bytes for lossless roundtrip.
type MsgRegionRouteSet struct {
	Header *MsgHeader `json:"header"`
	Body   []byte     `json:"body"`
}

func (m *MsgRegionRouteSet) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) < 2 {
		return ErrDecodeMsg
	}
	if packet.Body[0] > 2 || int(packet.Body[1]) == 0 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	m.Body = append([]byte(nil), packet.Body...)
	return nil
}
func (m *MsgRegionRouteSet) Encode() ([]byte, error) {
	if m.Header == nil || len(m.Body) < 2 || m.Body[0] > 2 || m.Body[1] == 0 {
		return nil, ErrEncodeMsg
	}
	return writeHeader(m, hex.WriteBytes(nil, m.Body))
}
func (m *MsgRegionRouteSet) GetHeader() *MsgHeader        { return m.Header }
func (m *MsgRegionRouteSet) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }

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

// Msg0608 carries response records verbatim because each record's length
// depends on the query type and optional attributes. Count and body are kept
// separately to preserve and validate the standard envelope.
type Msg0608 struct {
	Header    *MsgHeader `json:"header"`
	QueryType uint8      `json:"queryType"`
	Count     uint32     `json:"count"`
	Records   []byte     `json:"records"`
}

func (m *Msg0608) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) < 5 {
		return ErrDecodeMsg
	}
	if packet.Body[0] < 1 || packet.Body[0] > 4 {
		return ErrDecodeMsg
	}
	m.Header, m.QueryType = packet.Header, packet.Body[0]
	idx := 1
	m.Count = hex.ReadDoubleWord(packet.Body, &idx)
	m.Records = append([]byte(nil), packet.Body[idx:]...)
	if m.Count == 0 && len(m.Records) != 0 {
		return ErrDecodeMsg
	}
	return nil
}
func (m *Msg0608) Encode() ([]byte, error) {
	if m.Header == nil || m.QueryType < 1 || m.QueryType > 4 || (m.Count == 0) != (len(m.Records) == 0) {
		return nil, ErrEncodeMsg
	}
	body := []byte{m.QueryType}
	body = hex.WriteDoubleWord(body, m.Count)
	body = hex.WriteBytes(body, m.Records)
	return writeHeader(m, body)
}
func (m *Msg0608) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0608) GenOutgoing(incoming JT808Msg) error {
	query, ok := incoming.(*Msg8608)
	if !ok {
		return ErrGenOutgoingMsg
	}
	m.Header = cloneReplyHeader(query.Header, 0x0608)
	m.QueryType = query.QueryType
	return nil
}
