package model

import (
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// LocationExtItem is one 0x0200 extended (附加信息) TLV item.
type LocationExtItem struct {
	ID     uint8  `json:"id"`     // 附加信息ID
	Length uint16 `json:"length"` // 附加信息长度
	Data   []byte `json:"data"`   // 附加信息内容
}

// 位置信息汇报
type Msg0200 struct {
	Header     *MsgHeader        `json:"header"`
	AlarmSign  uint32            `json:"alarmSign"`          // 报警标志位
	StatusSign uint32            `json:"statusSign"`         // 状态标志位
	Latitude   uint32            `json:"latitude"`           // 纬度，以度为单位的纬度值乘以10的6次方，精确到百万分之一度
	Longitude  uint32            `json:"longitude"`          // 精度，以度为单位的经度值乘以10的6次方，精确到百万分之一度
	Altitude   uint16            `json:"altitude"`           // 高程，海拔高度，单位为米(m)
	Speed      uint16            `json:"speed"`              // 速度，单位为0.1公里每小时(1/10km/h)
	Direction  uint16            `json:"direction"`          // 方向，0-359，正北为 0，顺时针
	Time       string            `json:"time"`               // YY-MM-DD-hh-mm-ss(GMT+8 时间)
	ExtItems   []LocationExtItem `json:"extItems,omitempty"` // 附加信息列表（TLV）
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
	m.ExtItems = nil
	for idx < len(pkt) {
		if idx+3 > len(pkt) {
			return ErrDecodeMsg
		}
		item := LocationExtItem{ID: hex.ReadByte(pkt, &idx), Length: hex.ReadWord(pkt, &idx)}
		if idx+int(item.Length) > len(pkt) {
			return ErrDecodeMsg
		}
		item.Data = hex.ReadBytes(pkt, &idx, int(item.Length))
		m.ExtItems = append(m.ExtItems, item)
	}
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
	for _, item := range m.ExtItems {
		pkt = hex.WriteByte(pkt, item.ID)
		pkt = hex.WriteWord(pkt, item.Length)
		pkt = hex.WriteBytes(pkt, item.Data)
	}

	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg0200) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0200) GenOutgoing(_ JT808Msg) error {
	return nil
}
