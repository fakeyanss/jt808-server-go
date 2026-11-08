package model

import (
	"encoding/binary"
	"fmt"
)

// PeripheralStatus 是 0xF7 外设状态查询的内容。
// 消息长度固定为 5：工作状态 BYTE + 报警状态 DWORD。
type PeripheralStatus struct {
	WorkStatus  uint8  `json:"workStatus"`
	AlarmStatus uint32 `json:"alarmStatus"`
}

// PeripheralSystem 是 0xF8 外设信息查询的内容。
// 六段内容均为“长度 BYTE + 内容”，这里保留原始字节，避免提前按 ASCII/GBK 解码丢信息。
type PeripheralSystem struct {
	CompanyName     []byte `json:"companyName"`
	ProductModel    []byte `json:"productModel"`
	HardwareVersion []byte `json:"hardwareVersion"`
	SoftwareVersion []byte `json:"softwareVersion"`
	DeviceID        []byte `json:"deviceId"`
	CustomerCode    []byte `json:"customerCode"`
}

// PeripheralItem 是 0x0900 中的单个外设消息。
//
// 结构由外层 Type 决定，而不是由 ID 决定：F7/F8 下即使外设 ID 未知也按同一内容结构解析。
// Raw 始终保留；Status/System 仅在解析成功时填充，未知类型或长度不符时降级为 Raw。
type PeripheralItem struct {
	ID     uint8             `json:"id"`
	Length uint8             `json:"length"`
	Raw    []byte            `json:"raw"`
	Status *PeripheralStatus `json:"status,omitempty"`
	System *PeripheralSystem `json:"system,omitempty"`
}

// Msg0900 是终端上报的外设透传消息（苏标 0x0900）。
//
// 线格式为：透传类型 BYTE + 消息列表总数 BYTE + 外设消息结构 × n，
// 每个外设消息结构为：外设 ID BYTE + 消息长度 BYTE + 消息内容。
type Msg0900 struct {
	Header *MsgHeader       `json:"header"`
	Type   uint8            `json:"type"`  // 透传类型：0xF7 状态查询、0xF8 信息查询
	Items  []PeripheralItem `json:"items"` // 有序切片，顺序即线格式顺序
}

func (m *Msg0900) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 2 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	m.Type = p.Body[0]
	count := int(p.Body[1])
	items := make([]PeripheralItem, 0, count)
	idx := 2
	for i := 0; i < count; i++ {
		if idx+2 > len(p.Body) {
			return fmt.Errorf("%w: 0x0900 truncated at peripheral item %d header", ErrDecodeMsg, i)
		}
		id := p.Body[idx]
		length := int(p.Body[idx+1])
		idx += 2
		if idx+length > len(p.Body) {
			return fmt.Errorf("%w: 0x0900 peripheral item %d declares %d bytes but only %d remain", ErrDecodeMsg, i, length, len(p.Body)-idx)
		}
		raw := append([]byte(nil), p.Body[idx:idx+length]...)
		idx += length

		item := PeripheralItem{ID: id, Length: uint8(length), Raw: raw}
		switch m.Type {
		case TransparentTypePeripheralStatus:
			// 长度不符时只保留 Raw，不因单个条目畸形导致整包失败。
			if length == 5 {
				item.Status = &PeripheralStatus{
					WorkStatus:  raw[0],
					AlarmStatus: binary.BigEndian.Uint32(raw[1:5]),
				}
			}
		case TransparentTypePeripheralInfo:
			if system, ok := decodePeripheralSystem(raw); ok {
				item.System = system
			}
		}
		items = append(items, item)
	}
	if idx != len(p.Body) {
		return fmt.Errorf("%w: 0x0900 has %d trailing bytes", ErrDecodeMsg, len(p.Body)-idx)
	}
	m.Items = items
	return nil
}

func (m *Msg0900) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	if len(m.Items) > 255 {
		return nil, fmt.Errorf("%w: 0x0900 supports at most 255 peripheral items, got %d", ErrEncodeMsg, len(m.Items))
	}
	body := make([]byte, 0, 2+len(m.Items)*8)
	body = append(body, m.Type, uint8(len(m.Items)))
	for i, item := range m.Items {
		content, err := item.encodeContent(m.Type)
		if err != nil {
			return nil, fmt.Errorf("%w: peripheral item %d: %v", ErrEncodeMsg, i, err)
		}
		if len(content) > 255 {
			return nil, fmt.Errorf("%w: peripheral item %d content is %d bytes, max 255", ErrEncodeMsg, i, len(content))
		}
		body = append(body, item.ID, uint8(len(content)))
		body = append(body, content...)
	}
	return writeHeader(m, body)
}

// encodeContent 按外层 Type 生成消息内容。
// 已解析出结构时优先使用结构（F7 固定 5 字节），否则回退到 Raw。
func (item PeripheralItem) encodeContent(transparentType uint8) ([]byte, error) {
	if transparentType == TransparentTypePeripheralStatus && item.Status != nil {
		content := make([]byte, 5)
		content[0] = item.Status.WorkStatus
		binary.BigEndian.PutUint32(content[1:], item.Status.AlarmStatus)
		return content, nil
	}
	if transparentType == TransparentTypePeripheralInfo && item.System != nil {
		return encodePeripheralSystem(item.System)
	}
	if item.Raw != nil {
		return append([]byte(nil), item.Raw...), nil
	}
	if item.Length > 0 {
		return nil, fmt.Errorf("declared length %d but no raw or structured content", item.Length)
	}
	return nil, nil
}

// decodePeripheralSystem 解析 F8 的 6 组“长度 + 内容”。
// 任何长度越界或尾部多余字节都视为格式不符，返回 false 由调用方降级到 Raw。
func decodePeripheralSystem(raw []byte) (*PeripheralSystem, bool) {
	fields := make([][]byte, 6)
	idx := 0
	for i := range fields {
		if idx >= len(raw) {
			return nil, false
		}
		length := int(raw[idx])
		idx++
		if idx+length > len(raw) {
			return nil, false
		}
		fields[i] = append([]byte(nil), raw[idx:idx+length]...)
		idx += length
	}
	if idx != len(raw) {
		return nil, false
	}
	return &PeripheralSystem{
		CompanyName:     fields[0],
		ProductModel:    fields[1],
		HardwareVersion: fields[2],
		SoftwareVersion: fields[3],
		DeviceID:        fields[4],
		CustomerCode:    fields[5],
	}, true
}

// encodePeripheralSystem 依次写入六段“长度 + 内容”，并回填各段与外层总长度。
func encodePeripheralSystem(system *PeripheralSystem) ([]byte, error) {
	fields := [][]byte{
		system.CompanyName,
		system.ProductModel,
		system.HardwareVersion,
		system.SoftwareVersion,
		system.DeviceID,
		system.CustomerCode,
	}
	content := make([]byte, 0, 64)
	for i, field := range fields {
		if len(field) > 255 {
			return nil, fmt.Errorf("F8 field %d is %d bytes, max 255", i, len(field))
		}
		content = append(content, uint8(len(field)))
		content = append(content, field...)
	}
	if len(content) > 255 {
		return nil, fmt.Errorf("F8 content is %d bytes, max 255", len(content))
	}
	return content, nil
}

func (m *Msg0900) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0900) GenOutgoing(_ JT808Msg) error {
	return nil
}
