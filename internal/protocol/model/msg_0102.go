package model

import (
	"strings"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// 终端鉴权
type Msg0102 struct {
	Header          *MsgHeader `json:"header"`
	AuthCodeLen     uint8      `json:"authCodeLen"`     // 鉴权码长度，byte，2019版本有
	AuthCode        string     `json:"authCode"`        // 鉴权码，string
	IMEI            string     `json:"imei"`            // 终端IMEI，byte(15)，2019版本有
	SoftwareVersion string     `json:"softwareVersion"` // 软件版本号，byte(20)，2019版本有
}

func (m *Msg0102) Decode(packet *PacketData) error {
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	ver := m.Header.Attr.VersionDesc
	if ver == Version2013 {
		m.AuthCode = hex.ReadString(pkt, &idx, int(m.Header.Attr.BodyLength)-idx)
	} else if ver == Version2019 {
		m.AuthCodeLen = hex.ReadByte(pkt, &idx)
		m.AuthCode = hex.ReadString(pkt, &idx, int(m.AuthCodeLen))
		m.IMEI = hex.ReadString(pkt, &idx, 15)
		m.SoftwareVersion = strings.TrimRight(hex.ReadString(pkt, &idx, 20), "\x00")
	}
	return nil
}

func (m *Msg0102) Encode() (pkt []byte, err error) {
	ver := m.Header.Attr.VersionDesc
	if ver == Version2013 || ver == Version2011 {
		pkt = append(pkt, []byte(m.AuthCode)...)
	} else if ver == Version2019 {
		if m.AuthCodeLen != uint8(len(m.AuthCode)) {
			m.AuthCodeLen = uint8(len(m.AuthCode))
		}
		pkt = hex.WriteByte(pkt, m.AuthCodeLen)
		pkt = hex.WriteString(pkt, m.AuthCode)
		pkt = hex.WriteString(pkt, m.IMEI)
		pkt = hex.WriteString(pkt, m.SoftwareVersion)
	}

	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg0102) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0102) GenOutgoing(incoming JT808Msg) error {
	in, ok := incoming.(*Msg8100)
	if !ok {
		return ErrGenOutgoingMsg
	}
	// 后置设置鉴权参数
	m.Header = in.Header
	m.Header.MsgID = 0x0102

	return nil
}
