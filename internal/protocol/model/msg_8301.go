package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// EventItem is one configurable event in 0x8301.
type EventItem struct {
	ID      uint8  `json:"id"`      // 事件ID
	Content string `json:"content"` // 事件内容 GBK
}

// Msg8301 sets terminal event items.
type Msg8301 struct {
	Header    *MsgHeader  `json:"header"`
	SetType   uint8       `json:"setType"` // 设置类型 0:删除全部 1:更新 2:追加 3:修改
	ItemCount uint8       `json:"itemCount"`
	Items     []EventItem `json:"items"`
}

func (m *Msg8301) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 2 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.SetType = hex.ReadByte(p.Body, &idx)
	m.ItemCount = hex.ReadByte(p.Body, &idx)
	m.Items = nil
	for n := 0; n < int(m.ItemCount); n++ {
		if idx+2 > len(p.Body) {
			return ErrDecodeMsg
		}
		item := EventItem{ID: hex.ReadByte(p.Body, &idx)}
		contentLen := int(hex.ReadByte(p.Body, &idx))
		if idx+contentLen > len(p.Body) {
			return ErrDecodeMsg
		}
		item.Content = hex.ReadGBK(p.Body, &idx, contentLen)
		m.Items = append(m.Items, item)
	}
	return nil
}

func (m *Msg8301) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt := []byte{m.SetType, uint8(len(m.Items))}
	for _, item := range m.Items {
		gbk := hex.WriteGBK(nil, item.Content)
		pkt = append(pkt, item.ID, uint8(len(gbk)))
		pkt = append(pkt, gbk...)
	}
	return writeHeader(m, pkt)
}

func (m *Msg8301) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8301) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
