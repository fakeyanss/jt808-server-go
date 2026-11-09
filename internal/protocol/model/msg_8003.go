package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg8003 asks a terminal to retransmit selected fragments of an original
// segmented message. Its body does not include the original message ID.
// 重传包总数在 2011/2013 版本为 BYTE，在 2019 版本为 WORD。
type Msg8003 struct {
	Header               *MsgHeader `json:"header"`
	OriginalSerialNumber uint16     `json:"originalSerialNumber"`
	RetransmitCount      uint16     `json:"retransmitCount"`
	PacketIDs            []uint16   `json:"packetIDs"`
}

func (m *Msg8003) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil {
		return ErrDecodeMsg
	}
	is2019 := packet.Header.Is2019()
	minLen := 3
	if is2019 {
		minLen = 4
	}
	if len(packet.Body) < minLen {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	idx := 0
	m.OriginalSerialNumber = hex.ReadWord(packet.Body, &idx)
	if is2019 {
		m.RetransmitCount = hex.ReadWord(packet.Body, &idx)
	} else {
		m.RetransmitCount = uint16(hex.ReadByte(packet.Body, &idx))
	}
	if len(packet.Body)-idx != int(m.RetransmitCount)*2 {
		return ErrDecodeMsg
	}
	m.PacketIDs = make([]uint16, 0, m.RetransmitCount)
	for i := 0; i < int(m.RetransmitCount); i++ {
		m.PacketIDs = append(m.PacketIDs, hex.ReadWord(packet.Body, &idx))
	}
	return nil
}

func (m *Msg8003) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	if len(m.PacketIDs) > 65535 {
		return nil, ErrEncodeMsg
	}
	if !m.Header.Is2019() && len(m.PacketIDs) > 255 {
		return nil, ErrEncodeMsg
	}
	pkt := hex.WriteWord(nil, m.OriginalSerialNumber)
	if m.Header.Is2019() {
		pkt = hex.WriteWord(pkt, uint16(len(m.PacketIDs)))
	} else {
		pkt = hex.WriteByte(pkt, uint8(len(m.PacketIDs)))
	}
	for _, id := range m.PacketIDs {
		pkt = hex.WriteWord(pkt, id)
	}
	return writeHeader(m, pkt)
}

func (m *Msg8003) GetHeader() *MsgHeader { return m.Header }

func (m *Msg8003) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
