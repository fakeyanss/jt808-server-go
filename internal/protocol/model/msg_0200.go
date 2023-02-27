package model

import (
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
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
	pkt, idx := packet.Body, 0
	m.AlarmSign = hex.ReadDoubleWord(pkt, &idx)
	m.StatusSign = hex.ReadDoubleWord(pkt, &idx)
	m.Latitude = hex.ReadDoubleWord(pkt, &idx)
	m.Longitude = hex.ReadDoubleWord(pkt, &idx)
	m.Altitude = hex.ReadWord(pkt, &idx)
	m.Speed = hex.ReadWord(pkt, &idx)
	m.Direction = hex.ReadWord(pkt, &idx)
	m.Time = hex.ReadBCD(pkt, &idx, 6)
	return nil
}

func (m *Msg0200) Encode() (pkt []byte, err error) {
	pkt = hex.WriteDoubleWord(pkt, m.AlarmSign)
	pkt = hex.WriteDoubleWord(pkt, m.StatusSign)
	pkt = hex.WriteDoubleWord(pkt, m.Latitude)
	pkt = hex.WriteDoubleWord(pkt, m.Longitude)
	pkt = hex.WriteWord(pkt, m.Altitude)
	pkt = hex.WriteWord(pkt, m.Speed)
	pkt = hex.WriteWord(pkt, m.Direction)
	pkt = hex.WriteBCD(pkt, m.Time)

	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg0200) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0200) GenOutgoing(incoming JT808Msg) error {
	return nil
}
