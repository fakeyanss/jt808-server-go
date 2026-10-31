package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// BatchLocationItem is one 0x0200 location body in a 0x0704 batch.
type BatchLocationItem struct {
	Length uint16  `json:"length"` // 位置汇报数据长度
	Body   []byte  `json:"body"`   // 0x0200 消息体
	Loc    Msg0200 `json:"loc"`    // 解析后的位置信息
}

// Msg0704 is a terminal batch positioning data upload.
type Msg0704 struct {
	Header    *MsgHeader          `json:"header"`
	ItemCount uint16              `json:"itemCount"` // 数据项数
	DataType  uint8               `json:"dataType"`  // 位置数据类型 0:正常 1:盲区补报
	Items     []BatchLocationItem `json:"items"`
}

func (m *Msg0704) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 3 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.ItemCount = hex.ReadWord(p.Body, &idx)
	m.DataType = hex.ReadByte(p.Body, &idx)
	m.Items = nil
	for n := 0; n < int(m.ItemCount); n++ {
		if idx+2 > len(p.Body) {
			return ErrDecodeMsg
		}
		item := BatchLocationItem{Length: hex.ReadWord(p.Body, &idx)}
		if idx+int(item.Length) > len(p.Body) {
			return ErrDecodeMsg
		}
		item.Body = append([]byte(nil), p.Body[idx:idx+int(item.Length)]...)
		if err := item.Loc.Decode(&PacketData{Header: p.Header, Body: item.Body}); err != nil {
			return err
		}
		idx += int(item.Length)
		m.Items = append(m.Items, item)
	}
	return nil
}

func (m *Msg0704) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt := hex.WriteWord(nil, m.ItemCount)
	pkt = hex.WriteByte(pkt, m.DataType)
	for _, item := range m.Items {
		pkt = hex.WriteWord(pkt, uint16(len(item.Body)))
		pkt = hex.WriteBytes(pkt, item.Body)
	}
	return writeHeader(m, pkt)
}

func (m *Msg0704) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0704) GenOutgoing(_ JT808Msg) error {
	return nil
}
