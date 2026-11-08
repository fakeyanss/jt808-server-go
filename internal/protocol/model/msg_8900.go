package model

import (
	"fmt"
)

// Msg8900 是平台下发的外设查询消息（苏标 0x8900）。
//
// 线格式为：透传类型 BYTE + 外设 ID 列表总数 BYTE + 外设 ID × n，
// 不携带任何消息体。0x8900 与 0x0900 是两套独立模型，不能复用同一 Schema。
type Msg8900 struct {
	Header *MsgHeader `json:"header"`
	Type   uint8      `json:"type"` // 透传类型：0xF7 状态查询、0xF8 信息查询
	IDs    []uint8    `json:"ids"`  // 外设 ID 列表，按请求顺序下发
}

func (m *Msg8900) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 2 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	m.Type = p.Body[0]
	count := int(p.Body[1])
	if len(p.Body)-2 != count {
		return fmt.Errorf("%w: 0x8900 declares %d peripheral IDs but carries %d bytes", ErrDecodeMsg, count, len(p.Body)-2)
	}
	if count > 0 {
		m.IDs = append([]byte(nil), p.Body[2:]...)
	}
	return nil
}

func (m *Msg8900) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	if len(m.IDs) > 255 {
		return nil, fmt.Errorf("%w: 0x8900 supports at most 255 peripheral IDs, got %d", ErrEncodeMsg, len(m.IDs))
	}
	body := make([]byte, 0, 2+len(m.IDs))
	body = append(body, m.Type, uint8(len(m.IDs)))
	body = append(body, m.IDs...)
	return writeHeader(m, body)
}

func (m *Msg8900) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg8900) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }

// 苏标外设透传类型。
const (
	TransparentTypePeripheralStatus uint8 = 0xF7 // 外设状态查询
	TransparentTypePeripheralInfo   uint8 = 0xF8 // 外设信息查询
)

// 苏标外设 ID。
const (
	PeripheralIDADAS uint8 = 0x64
	PeripheralIDDSM  uint8 = 0x65
	PeripheralIDTPMS uint8 = 0x66
	PeripheralIDBSD  uint8 = 0x67
)
