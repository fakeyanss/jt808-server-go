package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg8900 is a platform data downlink transparent-transmission command.
type Msg8900 struct {
	Header *MsgHeader `json:"header"`
	Type   uint32     `json:"type"` // 透传消息类型
	Data   []byte     `json:"data"` // 透传消息内容
}

func (m *Msg8900) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 4 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.Type = hex.ReadDoubleWord(p.Body, &idx)
	m.Data = hex.ReadBytes(p.Body, &idx, len(p.Body)-idx)
	return nil
}

func (m *Msg8900) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt := hex.WriteDoubleWord(nil, m.Type)
	pkt = append(pkt, m.Data...)
	return writeHeader(m, pkt)
}

func (m *Msg8900) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8900) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
