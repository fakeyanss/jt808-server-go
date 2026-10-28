package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg8802 searches stored multimedia items within a time range.
type Msg8802 struct {
	Header    *MsgHeader `json:"header"`
	MediaType uint8      `json:"mediaType"`
	ChannelID uint8      `json:"channelId"`
	EventID   uint8      `json:"eventId"`
	StartTime string     `json:"startTime"`
	EndTime   string     `json:"endTime"`
}

func (m *Msg8802) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) != 15 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	i := 0
	m.MediaType = hex.ReadByte(p.Body, &i)
	m.ChannelID = hex.ReadByte(p.Body, &i)
	m.EventID = hex.ReadByte(p.Body, &i)
	if m.MediaType > 2 {
		return ErrDecodeMsg
	}
	m.StartTime = hex.ReadBCD(p.Body, &i, 6)
	m.EndTime = hex.ReadBCD(p.Body, &i, 6)
	return nil
}
func (m *Msg8802) Encode() ([]byte, error) {
	if m.Header == nil || m.MediaType > 2 || len(m.StartTime) != 12 || len(m.EndTime) != 12 {
		return nil, ErrEncodeMsg
	}
	b := []byte{m.MediaType, m.ChannelID, m.EventID}
	b = hex.WriteBCD(b, m.StartTime)
	b = hex.WriteBCD(b, m.EndTime)
	return writeHeader(m, b)
}
func (m *Msg8802) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8802) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }

// Msg0802 returns a search result. Variable location report bodies are
// preserved as an opaque record block; Count is zero for an empty result.
type Msg0802 struct {
	Header       *MsgHeader `json:"header"`
	AnswerSerial uint16     `json:"answerSerial"`
	Count        uint16     `json:"count"`
	Records      []byte     `json:"records"`
}

func (m *Msg0802) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 4 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	i := 0
	m.AnswerSerial = hex.ReadWord(p.Body, &i)
	m.Count = hex.ReadWord(p.Body, &i)
	m.Records = append([]byte(nil), p.Body[i:]...)
	if m.Count == 0 && len(m.Records) > 0 {
		return ErrDecodeMsg
	}
	return nil
}
func (m *Msg0802) Encode() ([]byte, error) {
	if m.Header == nil || (m.Count == 0 && len(m.Records) > 0) {
		return nil, ErrEncodeMsg
	}
	b := hex.WriteWord(nil, m.AnswerSerial)
	b = hex.WriteWord(b, m.Count)
	b = append(b, m.Records...)
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
