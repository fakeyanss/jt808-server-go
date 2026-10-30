package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// MediaSearchItem is one fixed-width 0x0802 result (media descriptor and the
// 0x0200 location body captured when recording started).
type MediaSearchItem struct {
	MediaID   uint32  `json:"mediaId"`
	MediaType uint8   `json:"mediaType"`
	ChannelID uint8   `json:"channelId"`
	EventID   uint8   `json:"eventId"`
	Location  Msg0200 `json:"location"`
}

// Msg0802 returns typed, fixed-width search records.
type Msg0802 struct {
	Header       *MsgHeader        `json:"header"`
	AnswerSerial uint16            `json:"answerSerial"`
	Items        []MediaSearchItem `json:"items"`
}

func (m *Msg0802) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 4 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	i := 0
	m.AnswerSerial = hex.ReadWord(p.Body, &i)
	count := int(hex.ReadWord(p.Body, &i))
	if len(p.Body) != i+count*35 {
		return ErrDecodeMsg
	}
	m.Items = make([]MediaSearchItem, 0, count)
	for n := 0; n < count; n++ {
		item := MediaSearchItem{MediaID: hex.ReadDoubleWord(p.Body, &i), MediaType: hex.ReadByte(p.Body, &i), ChannelID: hex.ReadByte(p.Body, &i), EventID: hex.ReadByte(p.Body, &i)}
		if item.MediaID == 0 || item.MediaType > 2 {
			return ErrDecodeMsg
		}
		loc := append([]byte(nil), p.Body[i:i+28]...)
		if err := item.Location.Decode(&PacketData{Header: p.Header, Body: loc}); err != nil {
			return err
		}
		i += 28
		m.Items = append(m.Items, item)
	}
	return nil
}
func (m *Msg0802) Encode() ([]byte, error) {
	if m.Header == nil || len(m.Items) > 65535 {
		return nil, ErrEncodeMsg
	}
	b := hex.WriteWord(nil, m.AnswerSerial)
	b = hex.WriteWord(b, uint16(len(m.Items)))
	for _, item := range m.Items {
		if item.MediaID == 0 || item.MediaType > 2 {
			return nil, ErrEncodeMsg
		}
		b = hex.WriteDoubleWord(b, item.MediaID)
		b = append(b, item.MediaType, item.ChannelID, item.EventID)
		loc := item.Location
		if len(loc.Time) != 12 {
			return nil, ErrEncodeMsg
		}
		b = hex.WriteDoubleWord(b, loc.AlarmSign)
		b = hex.WriteDoubleWord(b, loc.StatusSign)
		b = hex.WriteDoubleWord(b, loc.Latitude)
		b = hex.WriteDoubleWord(b, loc.Longitude)
		b = hex.WriteWord(b, loc.Altitude)
		b = hex.WriteWord(b, loc.Speed)
		b = hex.WriteWord(b, loc.Direction)
		b = hex.WriteBCD(b, loc.Time)
	}
	return writeHeader(m, b)
}
func (m *Msg0802) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0802) GenOutgoing(incoming JT808Msg) error {
	q, ok := incoming.(*Msg8802)
	if !ok {
		return ErrGenOutgoingMsg
	}
	m.Header = cloneReplyHeader(q.Header, 0x0802)
	m.AnswerSerial = q.Header.SerialNumber
	return nil
}
