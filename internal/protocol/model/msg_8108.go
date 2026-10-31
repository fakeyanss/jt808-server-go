package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg8108 is a platform terminal-upgrade command.
type Msg8108 struct {
	Header      *MsgHeader `json:"header"`
	UpgradeType uint8      `json:"upgradeType"` // 0:终端 1:IC卡读卡器 2:北斗
	MakerID     string     `json:"makerId"`     // 制造商ID 5字节
	Version     string     `json:"version"`     // 版本号
	Data        []byte     `json:"data"`        // 升级数据包
}

func (m *Msg8108) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 8 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.UpgradeType = hex.ReadByte(p.Body, &idx)
	m.MakerID = hex.ReadString(p.Body, &idx, 5)
	verLen := int(hex.ReadByte(p.Body, &idx))
	if idx+verLen > len(p.Body) {
		return ErrDecodeMsg
	}
	m.Version = hex.ReadGBK(p.Body, &idx, verLen)
	if idx+4 > len(p.Body) {
		return ErrDecodeMsg
	}
	dataLen := int(hex.ReadDoubleWord(p.Body, &idx))
	if idx+dataLen > len(p.Body) {
		return ErrDecodeMsg
	}
	m.Data = hex.ReadBytes(p.Body, &idx, dataLen)
	return nil
}

func (m *Msg8108) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt := []byte{m.UpgradeType}
	maker := m.MakerID
	if len(maker) > 5 {
		maker = maker[:5]
	}
	for len(maker) < 5 {
		maker += "\x00"
	}
	pkt = append(pkt, maker...)
	verGBK := hex.WriteGBK(nil, m.Version)
	pkt = append(pkt, uint8(len(verGBK)))
	pkt = append(pkt, verGBK...)
	pkt = hex.WriteDoubleWord(pkt, uint32(len(m.Data)))
	pkt = append(pkt, m.Data...)
	return writeHeader(m, pkt)
}

func (m *Msg8108) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8108) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
