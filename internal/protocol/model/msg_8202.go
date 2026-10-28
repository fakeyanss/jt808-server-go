package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg8202 controls temporary location reporting. Interval zero stops tracking
// and omits the validity period, as specified by JT/T 808.
type Msg8202 struct {
	Header         *MsgHeader `json:"header"`
	TimeInterval   uint16     `json:"timeInterval"`
	ValidityPeriod uint32     `json:"validityPeriod"`
}

func (m *Msg8202) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || (len(packet.Body) != 2 && len(packet.Body) != 6) {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	idx := 0
	m.TimeInterval = hex.ReadWord(packet.Body, &idx)
	if m.TimeInterval == 0 {
		if len(packet.Body) != 2 {
			return ErrDecodeMsg
		}
		return nil
	}
	if len(packet.Body) != 6 {
		return ErrDecodeMsg
	}
	m.ValidityPeriod = hex.ReadDoubleWord(packet.Body, &idx)
	if m.ValidityPeriod == 0 {
		return ErrDecodeMsg
	}
	return nil
}

func (m *Msg8202) Encode() ([]byte, error) {
	pkt := hex.WriteWord(nil, m.TimeInterval)
	if m.TimeInterval != 0 {
		if m.ValidityPeriod == 0 {
			return nil, ErrEncodeMsg
		}
		pkt = hex.WriteDoubleWord(pkt, m.ValidityPeriod)
	}
	return writeHeader(m, pkt)
}
func (m *Msg8202) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8202) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
