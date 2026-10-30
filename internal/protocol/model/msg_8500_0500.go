package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// Msg8500 is a platform vehicle-control command.
type Msg8500 struct {
	Header       *MsgHeader `json:"header"`
	ControlFlags uint16     `json:"controlFlags"` // 控制标志 bit0:0=车门解锁 1=车门锁止
}

func (m *Msg8500) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 2 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.ControlFlags = hex.ReadWord(p.Body, &idx)
	return nil
}

func (m *Msg8500) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt := hex.WriteWord(nil, m.ControlFlags)
	return writeHeader(m, pkt)
}

func (m *Msg8500) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8500) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }

// Msg0500 is a terminal vehicle-control response.
type Msg0500 struct {
	Header       *MsgHeader `json:"header"`
	ControlFlags uint16     `json:"controlFlags"` // 控制标志（回显0x8500的控制标志）
}

func (m *Msg0500) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 2 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.ControlFlags = hex.ReadWord(p.Body, &idx)
	return nil
}

func (m *Msg0500) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt := hex.WriteWord(nil, m.ControlFlags)
	return writeHeader(m, pkt)
}

func (m *Msg0500) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0500) GenOutgoing(incoming JT808Msg) error {
	in, ok := incoming.(*Msg8500)
	if !ok {
		return ErrGenOutgoingMsg
	}
	m.ControlFlags = in.ControlFlags
	m.Header = in.Header
	m.Header.MsgID = 0x0500
	return nil
}
