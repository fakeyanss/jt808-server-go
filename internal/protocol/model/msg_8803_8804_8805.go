package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg8803 requests upload of stored media captured within the time range.
type Msg8803 struct {
	Header     *MsgHeader `json:"header"`
	MediaType  uint8      `json:"mediaType"`
	ChannelID  uint8      `json:"channelId"`
	EventID    uint8      `json:"eventId"`
	StartTime  string     `json:"startTime"`
	EndTime    string     `json:"endTime"`
	DeleteFlag uint8      `json:"deleteFlag"`
}

func (m *Msg8803) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) != 16 || p.Body[0] > 2 || p.Body[15] > 1 {
		return ErrDecodeMsg
	}
	m.Header, m.MediaType, m.ChannelID, m.EventID = p.Header, p.Body[0], p.Body[1], p.Body[2]
	i := 3
	m.StartTime, m.EndTime = hex.ReadBCD(p.Body, &i, 6), hex.ReadBCD(p.Body, &i, 6)
	m.DeleteFlag = p.Body[i]
	return nil
}
func (m *Msg8803) Encode() ([]byte, error) {
	if m.Header == nil || m.MediaType > 2 || m.DeleteFlag > 1 || len(m.StartTime) != 12 || len(m.EndTime) != 12 {
		return nil, ErrEncodeMsg
	}
	b := []byte{m.MediaType, m.ChannelID, m.EventID}
	b = hex.WriteBCD(b, m.StartTime)
	b = hex.WriteBCD(b, m.EndTime)
	b = append(b, m.DeleteFlag)
	return writeHeader(m, b)
}
func (m *Msg8803) GetHeader() *MsgHeader      { return m.Header }
func (m *Msg8803) GenOutgoing(JT808Msg) error { return ErrGenOutgoingMsg }

// Msg8804 starts or stops audio recording.
type Msg8804 struct {
	Header     *MsgHeader `json:"header"`
	Command    uint8      `json:"command"`
	Duration   uint16     `json:"duration"`
	Save       uint8      `json:"save"`
	SampleRate uint8      `json:"sampleRate"`
}

func (m *Msg8804) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) != 5 || p.Body[0] > 1 || p.Body[3] > 1 || p.Body[4] > 3 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	i := 0
	m.Command = hex.ReadByte(p.Body, &i)
	m.Duration = hex.ReadWord(p.Body, &i)
	m.Save = hex.ReadByte(p.Body, &i)
	m.SampleRate = hex.ReadByte(p.Body, &i)
	return nil
}
func (m *Msg8804) Encode() ([]byte, error) {
	if m.Header == nil || m.Command > 1 || m.Save > 1 || m.SampleRate > 3 {
		return nil, ErrEncodeMsg
	}
	b := []byte{m.Command}
	b = hex.WriteWord(b, m.Duration)
	b = append(b, m.Save, m.SampleRate)
	return writeHeader(m, b)
}
func (m *Msg8804) GetHeader() *MsgHeader      { return m.Header }
func (m *Msg8804) GenOutgoing(JT808Msg) error { return ErrGenOutgoingMsg }

// Msg8805 requests upload of one stored media item.
type Msg8805 struct {
	Header     *MsgHeader `json:"header"`
	MediaID    uint32     `json:"mediaId"`
	DeleteFlag uint8      `json:"deleteFlag"`
}

func (m *Msg8805) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) != 5 {
		return ErrDecodeMsg
	}
	i := 0
	m.Header = p.Header
	m.MediaID = hex.ReadDoubleWord(p.Body, &i)
	m.DeleteFlag = p.Body[i]
	if m.MediaID == 0 || m.DeleteFlag > 1 {
		return ErrDecodeMsg
	}
	return nil
}
func (m *Msg8805) Encode() ([]byte, error) {
	if m.Header == nil || m.MediaID == 0 || m.DeleteFlag > 1 {
		return nil, ErrEncodeMsg
	}
	b := hex.WriteDoubleWord(nil, m.MediaID)
	b = append(b, m.DeleteFlag)
	return writeHeader(m, b)
}
func (m *Msg8805) GetHeader() *MsgHeader      { return m.Header }
func (m *Msg8805) GenOutgoing(JT808Msg) error { return ErrGenOutgoingMsg }
