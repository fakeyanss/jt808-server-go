package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg0705 reports fixed-width CAN items. The top three CAN-ID bits carry
// channel, frame type, and sampling method; the low 29 bits are the CAN ID.
type CANDataItem struct {
	CANID uint32  `json:"canId"`
	Data  [8]byte `json:"data"`
}
type Msg0705 struct {
	Header    *MsgHeader    `json:"header"`
	Count     uint16        `json:"count"`
	FirstTime string        `json:"firstTime"`
	Items     []CANDataItem `json:"items"`
}

func (m *Msg0705) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 7 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	i := 0
	m.Count = hex.ReadWord(p.Body, &i)
	if m.Count == 0 || len(p.Body) != 7+int(m.Count)*12 {
		return ErrDecodeMsg
	}
	m.FirstTime = hex.ReadBCD(p.Body, &i, 5)
	m.Items = make([]CANDataItem, 0, m.Count)
	for n := 0; n < int(m.Count); n++ {
		item := CANDataItem{CANID: hex.ReadDoubleWord(p.Body, &i)}
		copy(item.Data[:], hex.ReadBytes(p.Body, &i, 8))
		m.Items = append(m.Items, item)
	}
	return nil
}
func (m *Msg0705) Encode() ([]byte, error) {
	if m.Header == nil || len(m.Items) == 0 || len(m.Items) > 65535 || len(m.FirstTime) != 10 {
		return nil, ErrEncodeMsg
	}
	b := hex.WriteWord(nil, uint16(len(m.Items)))
	b = hex.WriteBCD(b, m.FirstTime)
	for _, it := range m.Items {
		b = hex.WriteDoubleWord(b, it.CANID)
		b = append(b, it.Data[:]...)
	}
	return writeHeader(m, b)
}
func (m *Msg0705) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg0705) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
