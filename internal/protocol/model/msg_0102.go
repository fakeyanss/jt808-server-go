package model

import "bytes"

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

	pkt := packet.Body

	idx := 0
	ver := m.Header.Attr.VersionDesc
	if ver == Version2013 {
		m.AuthCode = string(pkt)
	} else if ver == Version2019 {
		m.AuthCodeLen = pkt[idx]
		idx++
		m.AuthCode = string(pkt[idx : idx+int(m.AuthCodeLen)])
		idx += int(m.AuthCodeLen)

		m.IMEI = string(pkt[idx:15])
		idx += 15

		m.SoftwareVersion = string(bytes.TrimRight(pkt[idx:idx+20], "\x00"))
		idx += 20
	}

	// ac := make([]byte, 0)
	// for ; idx < len(pkt) && pkt[idx] != 0xFF; idx++ {
	// 	ac = append(ac, pkt[idx])
	// }
	// m.AuthCode = string(ac)

	// idx++

	// imei := make([]byte, 0)
	// for ; idx < len(pkt) && pkt[idx] != 0xFF; idx++ {
	// 	imei = append(imei, pkt[idx])
	// }
	// m.IMEI = string(imei)

	// idx++

	// m.SoftwareVersion = string(pkt[idx:])`

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
		pkt = append(pkt, m.AuthCodeLen)
		pkt = append(pkt, []byte(m.AuthCode)...)

		pkt = append(pkt, []byte(m.IMEI)...)

		pkt = append(pkt, []byte(m.SoftwareVersion)...)
	}

	m.Header.Attr.BodyLength = uint16(len(pkt))
	headerPkt, err := m.Header.Encode()
	if err != nil {
		return nil, err
	}
	pkt = append(headerPkt, pkt...)
	return pkt, nil
}

func (m *Msg0102) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0102) GenOutgoing(incoming JT808Msg) error {
	return nil
}
