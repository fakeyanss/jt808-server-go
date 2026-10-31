package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg8801 requests an immediate capture; its 12-byte body follows table 74.
type Msg8801 struct {
	Header     *MsgHeader `json:"header"`
	ChannelID  uint8      `json:"channelId"`
	Command    uint16     `json:"command"`
	Interval   uint16     `json:"interval"`
	Save       uint8      `json:"save"`
	Resolution uint8      `json:"resolution"`
	Quality    uint8      `json:"quality"`
	Brightness uint8      `json:"brightness"`
	Contrast   uint8      `json:"contrast"`
	Saturation uint8      `json:"saturation"`
	Chroma     uint8      `json:"chroma"`
}

func (m *Msg8801) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) != 12 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	i := 0
	m.ChannelID = hex.ReadByte(p.Body, &i)
	m.Command = hex.ReadWord(p.Body, &i)
	m.Interval = hex.ReadWord(p.Body, &i)
	m.Save = hex.ReadByte(p.Body, &i)
	m.Resolution = hex.ReadByte(p.Body, &i)
	m.Quality = hex.ReadByte(p.Body, &i)
	m.Brightness = hex.ReadByte(p.Body, &i)
	m.Contrast = hex.ReadByte(p.Body, &i)
	m.Saturation = hex.ReadByte(p.Body, &i)
	m.Chroma = hex.ReadByte(p.Body, &i)
	if m.ChannelID == 0 || m.Save > 1 || m.Quality < 1 || m.Quality > 10 || m.Contrast > 127 || m.Saturation > 127 || (m.Resolution != 0 && (m.Resolution < 1 || m.Resolution > 8) && m.Resolution != 0xff) {
		return ErrDecodeMsg
	}
	return nil
}
func (m *Msg8801) Encode() ([]byte, error) {
	if m.Header == nil || m.ChannelID == 0 || m.Save > 1 || m.Quality < 1 || m.Quality > 10 || m.Contrast > 127 || m.Saturation > 127 {
		return nil, ErrEncodeMsg
	}
	if m.Resolution != 0 && (m.Resolution < 1 || m.Resolution > 8) && m.Resolution != 0xff {
		return nil, ErrEncodeMsg
	}
	b := []byte{m.ChannelID}
	b = hex.WriteWord(b, m.Command)
	b = hex.WriteWord(b, m.Interval)
	b = append(b, m.Save, m.Resolution, m.Quality, m.Brightness, m.Contrast, m.Saturation, m.Chroma)
	return writeHeader(m, b)
}
func (m *Msg8801) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8801) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
