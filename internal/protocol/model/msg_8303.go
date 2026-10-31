package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// InfoMenuItem is one configurable menu entry in 0x8303.
type InfoMenuItem struct {
	ID      uint32 `json:"id"`      // 菜单ID
	Content string `json:"content"` // 菜单内容 GBK
}

// Msg8303 sets the terminal information on-demand menu.
type Msg8303 struct {
	Header  *MsgHeader     `json:"header"`
	SetType uint8          `json:"setType"` // 设置类型 0:删除全部 1:更新 2:追加 3:修改
	Items   []InfoMenuItem `json:"items"`
}

func (m *Msg8303) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 2 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.SetType = hex.ReadByte(p.Body, &idx)
	count := int(hex.ReadByte(p.Body, &idx))
	m.Items = nil
	for n := 0; n < count; n++ {
		if idx+5 > len(p.Body) {
			return ErrDecodeMsg
		}
		item := InfoMenuItem{ID: hex.ReadDoubleWord(p.Body, &idx)}
		contentLen := int(hex.ReadByte(p.Body, &idx))
		if idx+contentLen > len(p.Body) {
			return ErrDecodeMsg
		}
		item.Content = hex.ReadGBK(p.Body, &idx, contentLen)
		m.Items = append(m.Items, item)
	}
	return nil
}

func (m *Msg8303) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt := []byte{m.SetType, uint8(len(m.Items))}
	for _, item := range m.Items {
		gbk := hex.WriteGBK(nil, item.Content)
		pkt = hex.WriteDoubleWord(pkt, item.ID)
		pkt = append(pkt, uint8(len(gbk)))
		pkt = append(pkt, gbk...)
	}
	return writeHeader(m, pkt)
}

func (m *Msg8303) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8303) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
