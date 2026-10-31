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
