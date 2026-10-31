package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

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
