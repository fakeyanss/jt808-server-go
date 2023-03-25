package model

import (
	"fmt"
	"strings"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/codec/region"
)

// 终端注册
type Msg0100 struct {
	Header         *MsgHeader `json:"header"`
	ProvinceID     uint16     `json:"provinceId"`     // 省域ID，GBT2260 行政区号6位前2位。
	CityID         uint16     `json:"cityId"`         // 市县域ID，GBT2260 行政区号6位后4位
	ManufacturerID string     `json:"manufacturerId"` // 制造商ID
	DeviceMode     string     `json:"deviceMode"`     // 终端型号，2011版本8位，2013版本20位
	DeviceID       string     `json:"deviceId"`       // 终端ID，大写字母和数字

	// 车牌颜色
	//   2013版本按照JT415-2006定义，5.4.12节，0=未上牌，1=蓝，2=黄，3=黑，4=白，9=其他
	//   2019版本按照JT697.7-2014定义，5.6节，0=为上牌，1=蓝，2=黄，3=黑，4=白，5=绿，9=其他
	PlateColor byte `json:"plateColor"`

	PlateNumber  string `json:"plateNumber"`  // 车牌号
	LocationDesc string `json:"locationDesc"` // 省市地域中文名称，通过GBT2260解析
}

func (m *Msg0100) Decode(packet *PacketData) error {
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	m.ProvinceID = hex.ReadWord(pkt, &idx)
	m.CityID = hex.ReadWord(pkt, &idx)

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
			ver = &[]VersionType{Version2011}[0]
		}
	} else {
		return ErrDecodeMsg
	}
	cutset := "\x00"
	m.ManufacturerID = strings.TrimRight(hex.ReadString(pkt, &idx, manuLen), cutset)
	m.DeviceMode = strings.TrimRight(hex.ReadString(pkt, &idx, modeLen), cutset)
	m.DeviceID = strings.TrimRight(hex.ReadString(pkt, &idx, idLen), cutset)

	m.PlateColor = hex.ReadByte(pkt, &idx)
	m.PlateNumber = hex.ReadGBK(pkt, &idx, int(m.Header.Attr.BodyLength)-idx)
	m.LocationDesc = region.Parse(fmt.Sprintf("%02d%04d", m.ProvinceID, m.CityID)).Name

	return nil
}

func (m *Msg0100) Encode() (pkt []byte, err error) {
	pkt = hex.WriteWord(pkt, m.ProvinceID)
	pkt = hex.WriteWord(pkt, m.CityID)

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
	if toFillLen < 0 {
		manu = manu[:manuLen]
	} else {
		for i := 0; i < toFillLen; i++ {
			manu = append(manu, fillByte)
		}
	}
	pkt = append(pkt, manu...)

	mode := []byte(m.DeviceMode)
	toFillLen = modeLen - len(mode)
	if toFillLen < 0 {
		mode = manu[:modeLen]
	} else {
		for i := 0; i < toFillLen; i++ {
			mode = append(mode, fillByte)
		}
	}
	pkt = append(pkt, mode...)

	id := []byte(m.DeviceID)
	toFillLen = idLen - len(id)
	if toFillLen < 0 {
		id = id[:idLen]
	} else {
		for i := 0; i < toFillLen; i++ {
			id = append(id, fillByte)
		}
	}
	pkt = append(pkt, id...)

	pkt = hex.WriteByte(pkt, m.PlateColor)
	pkt = hex.WriteGBK(pkt, m.PlateNumber)

	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg0100) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0100) GenOutgoing(incoming JT808Msg) error {
	return nil
}
