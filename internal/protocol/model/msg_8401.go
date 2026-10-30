package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// PhonebookContact is one entry in a 0x8401 phonebook.
type PhonebookContact struct {
	Flag  uint8  `json:"flag"`  // bit0:呼入 bit1:呼出
	Phone string `json:"phone"` // 电话号码
	Name  string `json:"name"`  // 联系人
}

// Msg8401 sets the terminal phonebook.
type Msg8401 struct {
	Header  *MsgHeader         `json:"header"`
	SetType uint8              `json:"setType"` // 0:删除全部 1:更新 2:追加 3:修改
	Items   []PhonebookContact `json:"items"`
}

func (m *Msg8401) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 2 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.SetType = hex.ReadByte(p.Body, &idx)
	count := int(hex.ReadByte(p.Body, &idx))
	m.Items = nil
	for n := 0; n < count; n++ {
		if idx+3 > len(p.Body) {
			return ErrDecodeMsg
		}
		c := PhonebookContact{Flag: hex.ReadByte(p.Body, &idx)}
		phoneLen := int(hex.ReadByte(p.Body, &idx))
		if idx+phoneLen > len(p.Body) {
			return ErrDecodeMsg
		}
		c.Phone = hex.ReadGBK(p.Body, &idx, phoneLen)
		if idx+1 > len(p.Body) {
			return ErrDecodeMsg
		}
		nameLen := int(hex.ReadByte(p.Body, &idx))
		if idx+nameLen > len(p.Body) {
			return ErrDecodeMsg
		}
		c.Name = hex.ReadGBK(p.Body, &idx, nameLen)
		m.Items = append(m.Items, c)
	}
	return nil
}

func (m *Msg8401) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt := []byte{m.SetType, uint8(len(m.Items))}
	for _, c := range m.Items {
		phoneGBK := hex.WriteGBK(nil, c.Phone)
		nameGBK := hex.WriteGBK(nil, c.Name)
		pkt = append(pkt, c.Flag, uint8(len(phoneGBK)))
		pkt = append(pkt, phoneGBK...)
		pkt = append(pkt, uint8(len(nameGBK)))
		pkt = append(pkt, nameGBK...)
	}
	return writeHeader(m, pkt)
}

func (m *Msg8401) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8401) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
