package model

import (
	"bytes"
	"encoding/binary"

	"github.com/pkg/errors"

	"github.com/fakeYanss/jt808-server-go/internal/codec/bcd"
	"github.com/fakeYanss/jt808-server-go/internal/codec/gbk"
)

var ErrDecodeMsg = errors.New("Fail to decode Msg")

type JT808Msg interface {
	Decode(*PacketData) error // Packet -> JT808Msg
	GetHeader() *MsgHeader
}

// 终端通用应答
type Msg0001 struct {
	Header             *MsgHeader `json:"header"`
	AnswerSerialNumber uint16     `json:"answerSerialNumber"` // 应答流水号，对应平台消息的流水号
	AnswerMessageID    uint16     `json:"answerMessageId"`    // 应答ID，对应平台消息的ID
	Result             uint8      `json:"result"`             // 结果，0成功/确认，1失败，2消息有误，3不支持
}

func (m *Msg0001) Decode(packet *PacketData) error {
	m.Header = packet.Header

	pkt := packet.Body
	idx := 0

	m.AnswerSerialNumber = binary.BigEndian.Uint16(pkt[idx : idx+2])
	idx += 2

	m.AnswerMessageID = binary.BigEndian.Uint16(pkt[idx : idx+2])
	idx += 2

	m.Result = pkt[idx]
	idx++

	return nil
}

func (m *Msg0001) GetHeader() *MsgHeader {
	return m.Header
}

// 终端心跳
type Msg0002 struct {
	Header *MsgHeader `json:"header"`
	// 消息体为空
}

func (m *Msg0002) Decode(packet *PacketData) error {
	m.Header = packet.Header
	return nil
}

func (m *Msg0002) GetHeader() *MsgHeader {
	return m.Header
}

// 终端注销
type Msg0003 struct {
	Header *MsgHeader `json:"header"`
	// 消息体为空
}

func (m *Msg0003) Decode(packet *PacketData) error {
	m.Header = packet.Header
	return nil
}

func (m *Msg0003) GetHeader() *MsgHeader {
	return m.Header
}

// 查询服务器时间请求，2019版消息
type Msg0004 struct {
	Header *MsgHeader `json:"header"`
	// 消息体为空
}

func (m *Msg0004) Decode(packet *PacketData) error {
	m.Header = packet.Header
	return nil
}

func (m *Msg0004) GetHeader() *MsgHeader {
	return m.Header
}

// 终端注册
type Msg0100 struct {
	Header         *MsgHeader `json:"header"`
	ProvinceID     uint16     `json:"provinceId"`     // 省域ID，GBT2260 行政区号6位前2位
	CityID         uint16     `json:"cityId"`         // 市县域ID，GBT2260 行政区号6位后4位
	ManufacturerID string     `json:"manufacturerId"` // 制造商ID
	DeviceMode     string     `json:"deviceMode"`     // 终端型号
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

	if m.Header.Attr.VersionDesc == Version2019 {
		m.ManufacturerID = string(bytes.TrimRight(pkt[idx:idx+11], "\x00"))
		idx += 11

		m.DeviceMode = string(bytes.TrimRight(pkt[idx:idx+30], "\x00"))
		idx += 30

		m.DeviceID = string(bytes.TrimRight(pkt[idx:idx+30], "\x00"))
		idx += 30
	} else if m.Header.Attr.VersionDesc == Version2013 {
		m.ManufacturerID = string(pkt[idx : idx+5])
		idx += 5

		m.DeviceMode = string(bytes.TrimRight(pkt[idx:idx+20], "\x00"))
		idx += 20

		m.DeviceID = string(bytes.TrimRight(pkt[idx:idx+7], "\x00"))
		idx += 7
	} else {
		return ErrDecodeHeader
	}

	m.PlateColor = pkt[idx]
	idx++

	plateRegion, err := gbk.GBKToUTF8(pkt[idx : idx+2])
	if err != nil {
		// 解析车牌region失败, 留空
		plateRegion = []byte{}
	}
	idx += 2
	m.PlateNumber = string(append(plateRegion, pkt[idx:]...))

	return nil
}

func (m *Msg0100) GetHeader() *MsgHeader {
	return m.Header
}

// 终端鉴权
type Msg0102 struct {
	Header          *MsgHeader `json:"header"`
	AuthCode        string     `json:"authCode"`        // 鉴权码
	IMEI            string     `json:"imei"`            // 终端IMEI
	SoftwareVersion string     `json:"softwareVersion"` // 软件版本号
}

func (m *Msg0102) Decode(packet *PacketData) error {
	m.Header = packet.Header

	pkt := packet.Body
	idx := 0

	// 鉴权码、IMEI、版本号，以0xFF分隔

	ac := make([]byte, 0)
	for ; idx < len(pkt) && pkt[idx] != 0xFF; idx++ {
		ac = append(ac, pkt[idx])
	}
	m.AuthCode = string(ac)

	idx++

	imei := make([]byte, 0)
	for ; idx < len(pkt) && pkt[idx] != 0xFF; idx++ {
		imei = append(imei, pkt[idx])
	}
	m.IMEI = string(imei)

	idx++

	m.SoftwareVersion = string(pkt[idx:])

	return nil
}

func (m *Msg0102) GetHeader() *MsgHeader {
	return m.Header
}

// 位置信息汇报
type Msg0200 struct {
	Header     *MsgHeader `json:"header"`
	AlarmSign  uint32     `json:"alarmSign"`  // 报警标志位
	StatusSign uint32     `json:"statusSign"` // 状态标志位
	Latitude   uint32     `json:"latitude"`   // 纬度，以度为单位的纬度值乘以10的6次方，精确到百万分之一度
	Longitude  uint32     `json:"longitude"`  // 精度，以度为单位的经度值乘以10的6次方，精确到百万分之一度
	Altitude   uint16     `json:"altitude"`   // 高程，海拔高度，单位为米(m)
	Speed      uint16     `json:"speed"`      // 速度，单位为0.1公里每小时(1/10km/h)
	Direction  uint16     `json:"direction"`  // 方向，0-359，正北为 0，顺时针
	Time       string     `json:"time"`       // YY-MM-DD-hh-mm-ss(GMT+8 时间)
}

func (m *Msg0200) Decode(packet *PacketData) error {
	m.Header = packet.Header

	pkt := packet.Body
	idx := 0

	m.AlarmSign = binary.BigEndian.Uint32(pkt[idx : idx+4])
	idx += 4

	m.StatusSign = binary.BigEndian.Uint32(pkt[idx : idx+4])
	idx += 4

	m.Latitude = binary.BigEndian.Uint32(pkt[idx : idx+4])
	idx += 4

	m.Longitude = binary.BigEndian.Uint32(pkt[idx : idx+4])
	idx += 4

	m.Altitude = binary.BigEndian.Uint16(pkt[idx : idx+2])
	idx += 2

	m.Speed = binary.BigEndian.Uint16(pkt[idx : idx+2])
	idx += 2

	m.Direction = binary.BigEndian.Uint16(pkt[idx : idx+2])
	idx += 2

	m.Time = bcd.BCD2NumberStr(pkt[idx : idx+6])

	return nil
}

func (m *Msg0200) GetHeader() *MsgHeader {
	return m.Header
}
