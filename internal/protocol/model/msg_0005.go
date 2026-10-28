package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg0005 asks the platform to retransmit selected fragments of an original
// downlink message. Its body has the same layout as Msg8003.
type Msg0005 struct {
	Header               *MsgHeader `json:"header"`
	OriginalSerialNumber uint16     `json:"originalSerialNumber"`
	RetransmitCount      uint16     `json:"retransmitCount"`
	PacketIDs            []uint16   `json:"packetIDs"`
}

func (m *Msg0005) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) < 4 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	idx := 0
	m.OriginalSerialNumber = hex.ReadWord(packet.Body, &idx)
	m.RetransmitCount = hex.ReadWord(packet.Body, &idx)
	if len(packet.Body)-idx != int(m.RetransmitCount)*2 {
		return ErrDecodeMsg
	}
	m.PacketIDs = make([]uint16, 0, m.RetransmitCount)
	for i := 0; i < int(m.RetransmitCount); i++ {
		m.PacketIDs = append(m.PacketIDs, hex.ReadWord(packet.Body, &idx))
	}
	return nil
}

func (m *Msg0005) Encode() ([]byte, error) {
	if len(m.PacketIDs) > 65535 {
		return nil, ErrEncodeMsg
	}
	pkt := hex.WriteWord(nil, m.OriginalSerialNumber)
	pkt = hex.WriteWord(pkt, uint16(len(m.PacketIDs)))
	for _, id := range m.PacketIDs {
		pkt = hex.WriteWord(pkt, id)
	}
	return writeHeader(m, pkt)
}

func (m *Msg0005) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg0005) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
