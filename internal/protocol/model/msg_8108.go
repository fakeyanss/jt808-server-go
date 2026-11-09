package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg8108 is a platform terminal-upgrade command.
// MakerID 为定长字段：JT/T 808-2019 为 11 字节，2011/2013 为 5 字节。
type Msg8108 struct {
	Header      *MsgHeader `json:"header"`
	UpgradeType uint8      `json:"upgradeType"` // 0:终端 1:IC卡读卡器 2:北斗
	MakerID     string     `json:"makerId"`     // 制造商ID 2019:11字节，2011/2013:5字节
	Version     string     `json:"version"`     // 版本号
	Data        []byte     `json:"data"`        // 升级数据包
}

// makerIDLength 按消息头版本返回制造商 ID 字段长度。
// 2019 版本由消息体属性 bit14 判别，其余版本统一按 5 字节处理。
func makerIDLength(header *MsgHeader) int {
	if header != nil && header.Attr != nil && header.Attr.VersionDesc == Version2019 {
		return 11
	}
	return 5
}

func (m *Msg8108) Decode(p *PacketData) error {
	if p == nil || p.Header == nil {
		return ErrDecodeMsg
	}
	makerLen := makerIDLength(p.Header)
	if len(p.Body) < 1+makerLen+1+4 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.UpgradeType = hex.ReadByte(p.Body, &idx)
	m.MakerID = hex.ReadString(p.Body, &idx, makerLen)
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
	makerLen := makerIDLength(m.Header)
	maker := m.MakerID
	if len(maker) > makerLen {
		maker = maker[:makerLen]
	}
	for len(maker) < makerLen {
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
