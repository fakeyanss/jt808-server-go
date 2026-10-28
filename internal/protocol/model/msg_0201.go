package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg0201 returns the terminal's current location and answers a 0x8201 query.
type Msg0201 struct {
	Header             *MsgHeader `json:"header"`
	AnswerSerialNumber uint16     `json:"answerSerialNumber"`
	Location           *Msg0200   `json:"location"`
}

func (m *Msg0201) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) < 30 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	idx := 0
	m.AnswerSerialNumber = hex.ReadWord(packet.Body, &idx)
	m.Location = &Msg0200{}
	return m.Location.Decode(&PacketData{Header: packet.Header, Body: packet.Body[idx:]})
}

func (m *Msg0201) Encode() ([]byte, error) {
	if m.Location == nil {
		return nil, ErrEncodeMsg
	}
	pkt := hex.WriteWord(nil, m.AnswerSerialNumber)
	pkt = hex.WriteDoubleWord(pkt, m.Location.AlarmSign)
	pkt = hex.WriteDoubleWord(pkt, m.Location.StatusSign)
	pkt = hex.WriteDoubleWord(pkt, m.Location.Latitude)
	pkt = hex.WriteDoubleWord(pkt, m.Location.Longitude)
	pkt = hex.WriteWord(pkt, m.Location.Altitude)
	pkt = hex.WriteWord(pkt, m.Location.Speed)
	pkt = hex.WriteWord(pkt, m.Location.Direction)
	pkt = hex.WriteBCD(pkt, m.Location.Time)
	return writeHeader(m, pkt)
}

func (m *Msg0201) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0201) GenOutgoing(incoming JT808Msg) error {
	in, ok := incoming.(*Msg8201)
	if !ok {
		return ErrGenOutgoingMsg
	}
	m.Header = in.Header
	m.Header.MsgID = 0x0201
	m.AnswerSerialNumber = in.Header.SerialNumber
	return nil
}
