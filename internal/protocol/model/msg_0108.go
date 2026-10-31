package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg0108 is a terminal upgrade-result notification.
type Msg0108 struct {
	Header      *MsgHeader `json:"header"`
	UpgradeType uint8      `json:"upgradeType"`
	Result      uint8      `json:"result"` // 0:成功 1:失败 2:取消
}

func (m *Msg0108) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 2 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.UpgradeType = hex.ReadByte(p.Body, &idx)
	m.Result = hex.ReadByte(p.Body, &idx)
	return nil
}

func (m *Msg0108) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	return writeHeader(m, []byte{m.UpgradeType, m.Result})
}

func (m *Msg0108) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0108) GenOutgoing(_ JT808Msg) error {
	return nil
}
