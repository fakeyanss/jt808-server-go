package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg8300 is a platform text-information issue command.
type Msg8300 struct {
	Header   *MsgHeader `json:"header"`
	Flag     uint8      `json:"flag"`     // 标志位 bit0:紧急 bit1:TTS播报 bit2:广告屏显示 bit3:0导航屏/1终端屏
	TextType uint8      `json:"textType"` // 文本类型 1:通知 2:服务
	Text     string     `json:"text"`     // 文本信息 GBK
}

func (m *Msg8300) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 2 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.Flag = hex.ReadByte(p.Body, &idx)
	m.TextType = hex.ReadByte(p.Body, &idx)
	if idx < len(p.Body) {
		m.Text = hex.ReadGBK(p.Body, &idx, len(p.Body)-idx)
	}
	return nil
}

func (m *Msg8300) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt := []byte{m.Flag, m.TextType}
	pkt = hex.WriteGBK(pkt, m.Text)
	return writeHeader(m, pkt)
}

func (m *Msg8300) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8300) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }

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

// Msg0301 is a terminal event report.
type Msg0301 struct {
	Header  *MsgHeader `json:"header"`
	EventID uint8      `json:"eventId"` // 事件ID
}

func (m *Msg0301) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 1 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	m.EventID = p.Body[0]
	return nil
}

func (m *Msg0301) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	return writeHeader(m, []byte{m.EventID})
}

func (m *Msg0301) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0301) GenOutgoing(_ JT808Msg) error {
	return nil
}
