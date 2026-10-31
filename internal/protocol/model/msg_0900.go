package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg0900 is a terminal data uplink transparent-transmission report.
type Msg0900 struct {
	Header *MsgHeader `json:"header"`
	Type   uint32     `json:"type"` // 透传消息类型
	Data   []byte     `json:"data"` // 透传消息内容
}

func (m *Msg0900) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 4 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.Type = hex.ReadDoubleWord(p.Body, &idx)
	m.Data = hex.ReadBytes(p.Body, &idx, len(p.Body)-idx)
	return nil
}

func (m *Msg0900) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt := hex.WriteDoubleWord(nil, m.Type)
	pkt = append(pkt, m.Data...)
	return writeHeader(m, pkt)
}

func (m *Msg0900) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0900) GenOutgoing(_ JT808Msg) error {
	return nil
}
