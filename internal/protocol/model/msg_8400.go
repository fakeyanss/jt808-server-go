package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg8400 is a platform call-callback command.
type Msg8400 struct {
	Header *MsgHeader `json:"header"`
	Flag   uint8      `json:"flag"`  // 标志 0:普通通话 1:监听
	Phone  string     `json:"phone"` // 电话号码
}

func (m *Msg8400) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 2 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.Flag = hex.ReadByte(p.Body, &idx)
	phoneLen := int(hex.ReadByte(p.Body, &idx))
	if idx+phoneLen > len(p.Body) {
		return ErrDecodeMsg
	}
	m.Phone = hex.ReadGBK(p.Body, &idx, phoneLen)
	return nil
}

func (m *Msg8400) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	gbk := hex.WriteGBK(nil, m.Phone)
	pkt := []byte{m.Flag, uint8(len(gbk))}
	pkt = append(pkt, gbk...)
	return writeHeader(m, pkt)
}

func (m *Msg8400) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8400) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
