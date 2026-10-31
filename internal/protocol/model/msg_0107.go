package model

import (
	"fmt"
	"strings"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// Msg0107 carries the terminal's properties after a platform 0x8107 query.
type Msg0107 struct {
	Header                  *MsgHeader `json:"header"`
	TerminalType            uint16     `json:"terminalType"`
	ManufacturerID          string     `json:"manufacturerId"`
	TerminalModel           string     `json:"terminalModel"`
	TerminalID              string     `json:"terminalId"`
	ICCID                   string     `json:"iccid"`
	HardwareVersion         string     `json:"hardwareVersion"`
	FirmwareVersion         string     `json:"firmwareVersion"`
	GNSSProperties          uint8      `json:"gnssProperties"`
	CommunicationProperties uint8      `json:"communicationProperties"`
}

func (m *Msg0107) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || packet.Header.Attr == nil {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	body := packet.Body
	idx := 0
	modelLen, idLen := 20, 7
	if m.Header.Attr.VersionDesc == Version2019 {
		modelLen, idLen = 30, 30
	} else if m.Header.Attr.VersionDesc == Version2011 {
		modelLen = 8
	}
	if len(body) < 2+5+modelLen+idLen+10+4 {
		return ErrDecodeMsg
	}
	m.TerminalType = hex.ReadWord(body, &idx)
	m.ManufacturerID = trimFixed(hex.ReadString(body, &idx, 5))
	m.TerminalModel = trimFixed(hex.ReadString(body, &idx, modelLen))
	m.TerminalID = trimFixed(hex.ReadString(body, &idx, idLen))
	m.ICCID = hex.ReadBCD(body, &idx, 10)

	hardwareLen := int(hex.ReadByte(body, &idx))
	if hardwareLen > len(body)-idx-3 {
		return ErrDecodeMsg
	}
	m.HardwareVersion = hex.ReadString(body, &idx, hardwareLen)
	firmwareLen := int(hex.ReadByte(body, &idx))
	if firmwareLen != len(body)-idx-2 {
		return ErrDecodeMsg
	}
	m.FirmwareVersion = hex.ReadString(body, &idx, firmwareLen)
	m.GNSSProperties = hex.ReadByte(body, &idx)
	m.CommunicationProperties = hex.ReadByte(body, &idx)
	if idx != len(body) {
		return ErrDecodeMsg
	}
	return nil
}

func (m *Msg0107) Encode() ([]byte, error) {
	if m.Header == nil || m.Header.Attr == nil {
		return nil, ErrEncodeHeader
	}
	modelLen, idLen := 20, 7
	if m.Header.Attr.VersionDesc == Version2019 {
		modelLen, idLen = 30, 30
	} else if m.Header.Attr.VersionDesc != Version2011 && m.Header.Attr.VersionDesc != Version2013 {
		return nil, ErrEncodeHeader
	} else if m.Header.Attr.VersionDesc == Version2011 {
		modelLen = 8
	}
	if len(m.HardwareVersion) > 255 || len(m.FirmwareVersion) > 255 {
		return nil, ErrEncodeMsg
	}
	pkt := hex.WriteWord(nil, m.TerminalType)
	var err error
	for _, field := range []struct {
		value string
		width int
	}{{m.ManufacturerID, 5}, {m.TerminalModel, modelLen}, {m.TerminalID, idLen}} {
		pkt, err = appendFixedString(pkt, field.value, field.width)
		if err != nil {
			return nil, err
		}
	}
	iccid := m.ICCID
	if len(iccid) > 20 {
		return nil, ErrEncodeMsg
	}
	iccid = strings.Repeat("0", 20-len(iccid)) + iccid
	for _, digit := range iccid {
		if digit < '0' || digit > '9' {
			return nil, ErrEncodeMsg
		}
	}
	pkt = hex.WriteBCD(pkt, iccid)
	pkt = hex.WriteByte(pkt, uint8(len(m.HardwareVersion)))
	pkt = hex.WriteString(pkt, m.HardwareVersion)
	pkt = hex.WriteByte(pkt, uint8(len(m.FirmwareVersion)))
	pkt = hex.WriteString(pkt, m.FirmwareVersion)
	pkt = hex.WriteByte(pkt, m.GNSSProperties)
	pkt = hex.WriteByte(pkt, m.CommunicationProperties)
	return writeHeader(m, pkt)
}

func appendFixedString(dst []byte, value string, width int) ([]byte, error) {
	if len(value) > width {
		return nil, fmt.Errorf("fixed JT808 property exceeds %d bytes: %w", width, ErrEncodeMsg)
	}
	dst = hex.WriteString(dst, value)
	return append(dst, make([]byte, width-len(value))...), nil
}

func trimFixed(value string) string { return strings.TrimRight(value, "\x00") }

func (m *Msg0107) GetHeader() *MsgHeader { return m.Header }

func (m *Msg0107) GenOutgoing(incoming JT808Msg) error {
	in, ok := incoming.(*Msg8107)
	if !ok {
		return ErrGenOutgoingMsg
	}
	m.Header = in.Header
	m.Header.MsgID = 0x0107
	return nil
}
