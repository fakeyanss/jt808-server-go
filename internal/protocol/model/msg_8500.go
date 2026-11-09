package model

import (
	"github.com/pkg/errors"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// VehicleControlTypeDoor 是 2019 版本车门控制类型 ID。
const VehicleControlTypeDoor uint16 = 0x0001

// VehicleControl is one entry of the 2019 vehicle-control type list.
type VehicleControl struct {
	Type  uint16 `json:"type"`  // 控制类型ID，0x0001=车门
	Value uint8  `json:"value"` // 控制参数，0x0001 时 0=车门锁闭 1=车门开启
}

// Msg8500 is a platform vehicle-control command.
// 2011/2013 版本消息体为单字节控制标志；2019 版本改为控制类型数量加控制类型列表，
// 两者结构不同，必须按消息头版本分别编解码。
type Msg8500 struct {
	Header       *MsgHeader       `json:"header"`
	ControlFlags uint16           `json:"controlFlags"`       // 2011/2013 控制标志 bit0:0=车门解锁 1=车门锁止
	Controls     []VehicleControl `json:"controls,omitempty"` // 2019 控制类型列表
}

func (m *Msg8500) Decode(p *PacketData) error {
	if p == nil || p.Header == nil {
		return ErrDecodeMsg
	}
	is2019 := p.Header.Is2019()
	if !is2019 && len(p.Body) < 1 {
		return ErrDecodeMsg
	}
	if is2019 && len(p.Body) < 2 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	if !is2019 {
		m.ControlFlags = uint16(hex.ReadByte(p.Body, &idx))
		return nil
	}
	count := int(hex.ReadWord(p.Body, &idx))
	m.Controls = make([]VehicleControl, 0, count)
	for i := 0; i < count; i++ {
		if idx+3 > len(p.Body) {
			return ErrDecodeMsg
		}
		ctl := VehicleControl{Type: hex.ReadWord(p.Body, &idx)}
		if ctl.Type != VehicleControlTypeDoor {
			return errors.Wrapf(ErrDecodeMsg, "unsupported vehicle control type: 0x%04X", ctl.Type)
		}
		ctl.Value = hex.ReadByte(p.Body, &idx)
		m.Controls = append(m.Controls, ctl)
	}
	if idx != len(p.Body) {
		return errors.Wrap(ErrDecodeMsg, "trailing bytes after vehicle control list")
	}
	return nil
}

func (m *Msg8500) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	if !m.Header.Is2019() {
		if m.ControlFlags > 0xFF {
			return nil, ErrEncodeMsg
		}
		return writeHeader(m, hex.WriteByte(nil, uint8(m.ControlFlags)))
	}
	if len(m.Controls) > 65535 {
		return nil, ErrEncodeMsg
	}
	pkt := hex.WriteWord(nil, uint16(len(m.Controls)))
	for _, ctl := range m.Controls {
		if ctl.Type != VehicleControlTypeDoor {
			return nil, errors.Wrapf(ErrEncodeMsg, "unsupported vehicle control type: 0x%04X", ctl.Type)
		}
		pkt = hex.WriteWord(pkt, ctl.Type)
		pkt = hex.WriteByte(pkt, ctl.Value)
	}
	return writeHeader(m, pkt)
}

func (m *Msg8500) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8500) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }

// EchoControlFlags 返回 0x0500 应答中回显的控制标志。
// 2019 版本的单个控制类型没有对应字段，按车门控制类型映射为 2011/2013 标志位语义：
// 车门锁闭(0)对应 bit0=1，车门开启(1)对应 bit0=0。
func (m *Msg8500) EchoControlFlags() uint16 {
	if m.Header == nil || !m.Header.Is2019() {
		return m.ControlFlags
	}
	for _, ctl := range m.Controls {
		if ctl.Type == VehicleControlTypeDoor && ctl.Value == 0 {
			return 1
		}
	}
	return 0
}
