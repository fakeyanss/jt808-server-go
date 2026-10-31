package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg0303 is a terminal information on-demand / cancel report.
type Msg0303 struct {
	Header *MsgHeader `json:"header"`
	Action uint8      `json:"action"` // 0:取消 1:点播
	MenuID uint32     `json:"menuId"`
}

func (m *Msg0303) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 5 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.Action = hex.ReadByte(p.Body, &idx)
	m.MenuID = hex.ReadDoubleWord(p.Body, &idx)
	return nil
}

func (m *Msg0303) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt := []byte{m.Action}
	pkt = hex.WriteDoubleWord(pkt, m.MenuID)
	return writeHeader(m, pkt)
}

func (m *Msg0303) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0303) GenOutgoing(_ JT808Msg) error {
	return nil
}
