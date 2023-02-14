package model

import (
	"bytes"
	"encoding/binary"

	"github.com/fakeYanss/jt808-server-go/internal/codec/gbk"
)

// 终端注册
type Msg0100 struct {
	Header         *MsgHeader `json:"header"`
	ProvinceID     uint16     `json:"provinceId"`     // 省域ID，GBT2260 行政区号6位前2位。
	CityID         uint16     `json:"cityId"`         // 市县域ID，GBT2260 行政区号6位后4位
	ManufacturerID string     `json:"manufacturerId"` // 制造商ID
	DeviceMode     string     `json:"deviceMode"`     // 终端型号，2011版本8位，2013版本20位
	DeviceID       string     `json:"deviceId"`       // 终端ID，大写字母和数字
	PlateColor     byte       `json:"plateColor"`     // 车牌颜色，JTT415-2006定义，未上牌填0
	PlateNumber    string     `json:"plateNumber"`    // 车牌号
}

func (m *Msg0100) Decode(packet *PacketData) error {
	m.Header = packet.Header

	pkt := packet.Body
	idx := 0

	m.ProvinceID = binary.BigEndian.Uint16(pkt[idx : idx+2])
	idx += 2

	m.CityID = binary.BigEndian.Uint16(pkt[idx : idx+2])
	idx += 2

	ver := &m.Header.Attr.VersionDesc
	var manuLen, modeLen, idLen int
	if *ver == Version2019 {
		manuLen, modeLen, idLen = 11, 30, 30
	} else if *ver == Version2013 {
		manuLen, idLen = 5, 7
		remainLen := int(m.Header.Attr.BodyLength) - idx
		if remainLen > 5+20+7+1 { // 厂商+型号+ID+车牌颜色，2013版本至少33位
			modeLen = 20
		} else {
			modeLen = 8
			// ver = func(val VersionType) *VersionType { return &val }(Version2011)
			ver = &[]VersionType{Version2011}[0]
		}
	} else {
		return ErrDecodeMsg
	}
	trimStr := "\x00"
	m.ManufacturerID = string(bytes.TrimRight(pkt[idx:idx+manuLen], trimStr))
	idx += manuLen
	m.DeviceMode = string(bytes.TrimRight(pkt[idx:idx+modeLen], trimStr))
	idx += modeLen
	m.DeviceID = string(bytes.TrimRight(pkt[idx:idx+idLen], trimStr))
	idx += idLen

	m.PlateColor = pkt[idx]
	idx++

	plateRegion, err := gbk.GBK2UTF8(pkt[idx : idx+2])
	if err != nil {
		// 解析车牌region失败, 留空
		plateRegion = []byte{}
	}
	idx += 2
	m.PlateNumber = string(append(plateRegion, pkt[idx:]...))

	return nil
}

func (m *Msg0100) Encode() (pkt []byte, err error) {
	prov := make([]byte, 2)
	binary.BigEndian.PutUint16(prov, m.ProvinceID)
	pkt = append(pkt, prov...)

	city := make([]byte, 2)
	binary.BigEndian.PutUint16(city, m.CityID)
	pkt = append(pkt, city...)

	msgVer := m.Header.Attr.VersionDesc
	var manuLen, modeLen, idLen int // 设备厂商、型号、id长度
	if msgVer == Version2019 {
		manuLen, modeLen, idLen = 11, 30, 30
	} else if msgVer == Version2013 {
		manuLen, modeLen, idLen = 5, 20, 7
	} else if msgVer == Version2011 {
		manuLen, modeLen, idLen = 5, 8, 7
	} else {
		return nil, ErrEncodeHeader
	}
	var fillByte byte // '\x00'
	manu := []byte(m.ManufacturerID)
	toFillLen := manuLen - len(manu)
	for i := 0; i < toFillLen; i++ {
		manu = append(manu, fillByte)
	}
	pkt = append(pkt, manu...)

	mode := []byte(m.DeviceMode)
	toFillLen = modeLen - len(mode)
	for i := 0; i < toFillLen; i++ {
		mode = append(mode, fillByte)
	}
	pkt = append(pkt, mode...)

	id := []byte(m.DeviceID)
	toFillLen = idLen - len(id)
	for i := 0; i < toFillLen; i++ {
		id = append(id, fillByte)
	}
	pkt = append(pkt, id...)

	pkt = append(pkt, m.PlateColor)

	plateRegion, err := gbk.UTF82GBK([]byte(m.PlateNumber)[:3])
	if err != nil {
		plateRegion = []byte{}
	}
	pkt = append(pkt, plateRegion...)
	pkt = append(pkt, []byte(m.PlateNumber)[3:]...)

	m.Header.Attr.BodyLength = uint16(len(pkt))
	headerPkt, err := m.Header.Encode()
	if err != nil {
		return nil, err
	}
	pkt = append(headerPkt, pkt...)
	return pkt, nil
}

func (m *Msg0100) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0100) GenOutgoing(incoming JT808Msg) error {
	return nil
}
