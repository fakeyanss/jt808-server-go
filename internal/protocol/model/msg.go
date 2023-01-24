package model

import (
	"bytes"
	"encoding/binary"

	"github.com/fakeYanss/jt808-server-go/internal/util"
)

type JT808Msg interface {
	Decode([]byte) error // []byte -> struct
}

// 终端通用应答
type Msg0001 struct {
	MsgHeader
	AnswerSerialNumber uint16 `json:"answerSerialNumber"` // 应答流水号，对应平台消息的流水号
	AnswerMessageID    uint16 `json:"answerMessageId"`    // 应答ID，对应平台消息的ID
	Result             uint8  `json:"result"`             // 结果，0成功/确认，1失败，2消息有误，3不支持
}

func (m *Msg0001) Decode(pkt []byte) error {
	err := m.MsgHeader.Decode(pkt)
	if err != nil {
		return err
	}

	idx := m.idx

	m.AnswerSerialNumber = binary.BigEndian.Uint16(pkt[idx : idx+2])
	idx += 2

	m.AnswerMessageID = binary.BigEndian.Uint16(pkt[idx : idx+2])
	idx += 2

	m.Result = pkt[idx]
	idx++

	m.idx = idx
	return nil
}

// 终端心跳
type Msg0002 struct {
	MsgHeader
	// 消息体为空
}

func (m *Msg0002) Decode(pkt []byte) error {
	err := m.MsgHeader.Decode(pkt)
	if err != nil {
		return err
	}
	return nil
}

// 终端注销消息
type Msg0003 struct {
	MsgHeader
	// 消息体为空
}

func (m *Msg0003) Decode(pkt []byte) error {
	err := m.MsgHeader.Decode(pkt)
	if err != nil {
		return err
	}
	return nil
}

// 终端注册消息
type Msg0100 struct {
	MsgHeader
	ProvinceID     uint16 `json:"provinceId"`     // 省域ID，GBT2260 行政区号6位前2位
	CityID         uint16 `json:"cityId"`         // 市县域ID，GBT2260 行政区好6位后4位
	ManufacturerID string `json:"manufacturerId"` // 制造商ID
	DeviceMode     string `json:"deviceMode"`     // 终端型号
	DeviceID       string `json:"deviceId"`       // 终端ID，大写字母和数字
	PlateColor     byte   `json:"plateColor"`     // 车牌颜色，JTT415-2006定义，未上牌填0
	PlateNumber    string `json:"plateNumber"`    // 车牌号
}

func (m *Msg0100) Decode(pkt []byte) error {
	err := m.MsgHeader.Decode(pkt)
	if err != nil {
		return err
	}

	idx := m.idx

	m.ProvinceID = binary.BigEndian.Uint16(pkt[idx : idx+2])
	idx += 2

	m.CityID = binary.BigEndian.Uint16(pkt[idx : idx+2])
	idx += 2

	m.ManufacturerID = string(pkt[idx : idx+5])
	idx += 5

	m.DeviceMode = string(bytes.TrimRight(pkt[idx:idx+8], "\x00"))
	idx += 8

	m.DeviceID = string(pkt[idx : idx+7])
	idx += 7

	m.PlateColor = pkt[idx]
	idx++

	plateRegion, err := util.Utf8ToGbk(pkt[idx : idx+2])
	if err != nil {
		// return err // todo: gbk编码
	}
	idx += 2
	m.PlateNumber = string(plateRegion) + string(pkt[idx:])

	idx = int32(len(pkt) - 1)
	m.idx = idx

	return nil
}

// 终端鉴权消息
type Msg0102 struct {
	MsgHeader
	AuthCode        string `json:"authCode"`        // 鉴权码
	IMEI            string `json:"imei"`            // 终端IMEI
	SoftwareVersion string `json:"softwareVersion"` // 软件版本号
}

func (m *Msg0102) Decode(pkt []byte) error {
	err := m.MsgHeader.Decode(pkt)
	if err != nil {
		return err
	}

	idx := m.idx

	// 鉴权码、IMEI、版本号，以0xFF分隔

	ac := make([]byte, 0)
	for ; idx < int32(len(pkt)) && pkt[idx] != 0xFF; idx++ {
		ac = append(ac, pkt[idx])
	}
	m.AuthCode = string(ac)

	idx++

	imei := make([]byte, 0)
	for ; idx < int32(len(pkt)) && pkt[idx] != 0xFF; idx++ {
		imei = append(imei, pkt[idx])
	}
	m.IMEI = string(imei)

	idx++

	m.SoftwareVersion = string(pkt[idx:])

	return nil
}
