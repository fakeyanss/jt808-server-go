package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg8203 manually confirms one alarm message or all messages of an alarm type.
type Msg8203 struct {
	Header      *MsgHeader `json:"header"`
	AlarmSerial uint16     `json:"alarmSerial"`
	AlarmType   uint32     `json:"alarmType"`
}

func (m *Msg8203) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) != 6 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	idx := 0
	m.AlarmSerial = hex.ReadWord(packet.Body, &idx)
	m.AlarmType = hex.ReadDoubleWord(packet.Body, &idx)
	return nil
}
func (m *Msg8203) Encode() ([]byte, error) {
	pkt := hex.WriteWord(nil, m.AlarmSerial)
	pkt = hex.WriteDoubleWord(pkt, m.AlarmType)
	return writeHeader(m, pkt)
}
func (m *Msg8203) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8203) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
