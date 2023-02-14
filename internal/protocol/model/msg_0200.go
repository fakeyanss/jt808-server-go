package model

import (
	"encoding/binary"

	"github.com/fakeYanss/jt808-server-go/internal/codec/bcd"
)

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

func (m *Msg0200) Encode() (pkt []byte, err error) {
	// TODO
	m.Header.Attr.BodyLength = uint16(len(pkt))
	headerPkt, err := m.Header.Encode()
	if err != nil {
		return nil, err
	}
	pkt = append(headerPkt, pkt...)
	return pkt, nil
}

func (m *Msg0200) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0200) GenOutgoing(incoming JT808Msg) error {
	return nil
}
