package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg0701 is a terminal electronic waybill upload.
type Msg0701 struct {
	Header  *MsgHeader `json:"header"`
	Content string     `json:"content"` // 电子运单内容 GBK
}

func (m *Msg0701) Decode(p *PacketData) error {
	if p == nil || p.Header == nil {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	if len(p.Body) > 0 {
		idx := 0
		m.Content = hex.ReadGBK(p.Body, &idx, len(p.Body))
	}
	return nil
}

func (m *Msg0701) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt := hex.WriteGBK(nil, m.Content)
	return writeHeader(m, pkt)
}

func (m *Msg0701) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0701) GenOutgoing(_ JT808Msg) error {
	return nil
}
