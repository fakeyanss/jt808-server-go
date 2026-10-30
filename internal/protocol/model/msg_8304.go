package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg8304 is a platform information-service command.
type Msg8304 struct {
	Header   *MsgHeader `json:"header"`
	InfoType uint8      `json:"infoType"` // 信息类型 0:紧急 1:通知 2:服务
	Content  string     `json:"content"`  // 信息内容 GBK
}

func (m *Msg8304) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 3 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.InfoType = hex.ReadByte(p.Body, &idx)
	contentLen := int(hex.ReadWord(p.Body, &idx))
	if idx+contentLen > len(p.Body) {
		return ErrDecodeMsg
	}
	m.Content = hex.ReadGBK(p.Body, &idx, contentLen)
	return nil
}

func (m *Msg8304) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	gbk := hex.WriteGBK(nil, m.Content)
	pkt := []byte{m.InfoType}
	pkt = hex.WriteWord(pkt, uint16(len(gbk)))
	pkt = append(pkt, gbk...)
	return writeHeader(m, pkt)
}

func (m *Msg8304) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8304) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
