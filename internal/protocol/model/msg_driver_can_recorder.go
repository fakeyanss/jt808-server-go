package model

import (
	"bytes"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// Msg0702 reports driver IC-card state and identity.
type Msg0702 struct {
	Header            *MsgHeader `json:"header"`
	State             uint8      `json:"state"`
	Time              string     `json:"time"`
	ReadResult        uint8      `json:"readResult,omitempty"`
	Name              string     `json:"name,omitempty"`
	QualificationCode string     `json:"qualificationCode,omitempty"`
	IssuingAuthority  string     `json:"issuingAuthority,omitempty"`
	ValidUntil        string     `json:"validUntil,omitempty"`
	IdentityNumber    string     `json:"identityNumber,omitempty"`
}

func (m *Msg0702) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 7 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	i := 0
	m.State = hex.ReadByte(p.Body, &i)
	if m.State != 1 && m.State != 2 {
		return ErrDecodeMsg
	}
	m.Time = hex.ReadBCD(p.Body, &i, 6)
	if m.State == 2 {
		if i != len(p.Body) {
			return ErrDecodeMsg
		}
		return nil
	}
	if len(p.Body) < i+1 {
		return ErrDecodeMsg
	}
	m.ReadResult = hex.ReadByte(p.Body, &i)
	if m.ReadResult != 0 {
		if i != len(p.Body) {
			return ErrDecodeMsg
		}
		return nil
	}
	if len(p.Body) < i+1 {
		return ErrDecodeMsg
	}
	nameLen := int(hex.ReadByte(p.Body, &i))
	if len(p.Body) < i+nameLen+20+1+4+20 {
		return ErrDecodeMsg
	}
	m.Name = hex.ReadGBK(p.Body, &i, nameLen)
	m.QualificationCode = trimNUL(hex.ReadString(p.Body, &i, 20))
	authorityLen := int(hex.ReadByte(p.Body, &i))
	if len(p.Body) != i+authorityLen+4+20 {
		return ErrDecodeMsg
	}
	m.IssuingAuthority = hex.ReadGBK(p.Body, &i, authorityLen)
	m.ValidUntil = hex.ReadBCD(p.Body, &i, 4)
	m.IdentityNumber = trimNUL(hex.ReadString(p.Body, &i, 20))
	return nil
}
func (m *Msg0702) Encode() ([]byte, error) {
	if m.Header == nil || (m.State != 1 && m.State != 2) || len(m.Time) != 12 {
		return nil, ErrEncodeMsg
	}
	b := []byte{m.State}
	b = hex.WriteBCD(b, m.Time)
	if m.State == 1 {
		b = append(b, m.ReadResult)
		if m.ReadResult == 0 {
			name := hex.WriteGBK(nil, m.Name)
			authority := hex.WriteGBK(nil, m.IssuingAuthority)
			if len(name) > 255 || len(authority) > 255 || len(m.QualificationCode) > 20 || len(m.IdentityNumber) > 20 || len(m.ValidUntil) != 8 {
				return nil, ErrEncodeMsg
			}
			b = append(b, byte(len(name)))
			b = append(b, name...)
			b = appendPadded(b, m.QualificationCode, 20)
			b = append(b, byte(len(authority)))
			b = append(b, authority...)
			b = hex.WriteBCD(b, m.ValidUntil)
			b = appendPadded(b, m.IdentityNumber, 20)
		}
	}
	return writeHeader(m, b)
}
func (m *Msg0702) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0702) GenOutgoing(in JT808Msg) error {
	var header *MsgHeader
	switch request := in.(type) {
	case *Msg8702:
		header = request.Header
	case *MsgRawBody:
		header = request.Header
	default:
		return ErrGenOutgoingMsg
	}
	if header.MsgID != 0x8702 {
		return ErrGenOutgoingMsg
	}
	m.Header = cloneReplyHeader(header, 0x0702)
	m.State, m.Time, m.ReadResult = 1, "261028120000", 0
	m.Name, m.QualificationCode, m.IssuingAuthority = "模拟驾驶员", "00000000000000000001", "模拟发证机构"
	m.ValidUntil, m.IdentityNumber = "20301231", "00000000000000000001"
	return nil
}

func trimNUL(s string) string { return string(bytes.TrimRight([]byte(s), "\x00")) }
func appendPadded(dst []byte, value string, n int) []byte {
	b := []byte(value)
	dst = append(dst, b...)
	for i := len(b); i < n; i++ {
		dst = append(dst, 0)
	}
	return dst
}

// Msg0705 reports fixed-width CAN items. The top three CAN-ID bits carry
// channel, frame type, and sampling method; the low 29 bits are the CAN ID.
type CANDataItem struct {
	CANID uint32  `json:"canId"`
	Data  [8]byte `json:"data"`
}
type Msg0705 struct {
	Header    *MsgHeader    `json:"header"`
	Count     uint16        `json:"count"`
	FirstTime string        `json:"firstTime"`
	Items     []CANDataItem `json:"items"`
}

func (m *Msg0705) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 7 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	i := 0
	m.Count = hex.ReadWord(p.Body, &i)
	if m.Count == 0 || len(p.Body) != 7+int(m.Count)*12 {
		return ErrDecodeMsg
	}
	m.FirstTime = hex.ReadBCD(p.Body, &i, 5)
	m.Items = make([]CANDataItem, 0, m.Count)
	for n := 0; n < int(m.Count); n++ {
		item := CANDataItem{CANID: hex.ReadDoubleWord(p.Body, &i)}
		copy(item.Data[:], hex.ReadBytes(p.Body, &i, 8))
		m.Items = append(m.Items, item)
	}
	return nil
}
func (m *Msg0705) Encode() ([]byte, error) {
	if m.Header == nil || len(m.Items) == 0 || len(m.Items) > 65535 || len(m.FirstTime) != 10 {
		return nil, ErrEncodeMsg
	}
	b := hex.WriteWord(nil, uint16(len(m.Items)))
	b = hex.WriteBCD(b, m.FirstTime)
	for _, it := range m.Items {
		b = hex.WriteDoubleWord(b, it.CANID)
		b = append(b, it.Data[:]...)
	}
	return writeHeader(m, b)
}
func (m *Msg0705) GetHeader() *MsgHeader        { return m.Header }
func (m *Msg0705) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }

// MsgRecordCommand represents 0x8700/0x8701: a GB/T 19056 command byte and
// the corresponding standard-defined data block.
type MsgRecordCommand struct {
	Header    *MsgHeader `json:"header"`
	Command   uint8      `json:"command"`
	Data      []byte     `json:"data"`
	DataBlock any        `json:"dataBlock,omitempty"` // typed block, nil if command is unsupported
}

func (m *MsgRecordCommand) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 1 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	m.Command = p.Body[0]
	m.Data = append([]byte(nil), p.Body[1:]...)
	m.DataBlock = decodeRecorderCommandBlock(m.Command, m.Data)
	return nil
}
func (m *MsgRecordCommand) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	data, err := encodeRecorderBlock(m.DataBlock, m.Data)
	if err != nil {
		return nil, err
	}
	b := append([]byte{m.Command}, data...)
	return writeHeader(m, b)
}
func (m *MsgRecordCommand) GetHeader() *MsgHeader        { return m.Header }
func (m *MsgRecordCommand) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }

type Msg0700 struct {
	Header       *MsgHeader `json:"header"`
	AnswerSerial uint16     `json:"answerSerial"`
	Command      uint8      `json:"command"`
	Data         []byte     `json:"data"`
	DataBlock    any        `json:"dataBlock,omitempty"` // typed response block, nil if unsupported
}

func (m *Msg0700) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 3 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	i := 0
	m.AnswerSerial = hex.ReadWord(p.Body, &i)
	m.Command = hex.ReadByte(p.Body, &i)
	m.Data = append([]byte(nil), p.Body[i:]...)
	m.DataBlock = decodeRecorderResponseBlock(m.Command, m.Data)
	return nil
}
func (m *Msg0700) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	data, err := encodeRecorderBlock(m.DataBlock, m.Data)
	if err != nil {
		return nil, err
	}
	b := hex.WriteWord(nil, m.AnswerSerial)
	b = append(b, m.Command)
	b = append(b, data...)
	return writeHeader(m, b)
}
func (m *Msg0700) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0700) GenOutgoing(in JT808Msg) error {
	cmd, ok := in.(*MsgRecordCommand)
	if !ok {
		return ErrGenOutgoingMsg
	}
	m.Header = cloneReplyHeader(cmd.Header, 0x0700)
	m.AnswerSerial = cmd.Header.SerialNumber
	m.Command = cmd.Command
	return nil
}

// decodeRecorderCommandBlock decodes the GB/T 19056 data block of a platform
// command (0x8700/0x8701) into a typed struct. Unknown commands return nil.
func decodeRecorderCommandBlock(command uint8, data []byte) any {
	switch command {
	case RecorderCmdCollectInfo:
		b := &RecorderInfoRequest{}
		if b.Decode(data) == nil {
			return b
		}
	case RecorderCmdCollectFile:
		b := &RecorderFileRequest{}
		if b.Decode(data) == nil {
			return b
		}
	case RecorderCmdParamSet:
		b := &RecorderParamSetting{}
		if b.Decode(data) == nil {
			return b
		}
	case RecorderCmdPositionTestOn, RecorderCmdPositionTestOff:
		b := &RecorderPositionTestRequest{}
		if b.Decode(data) == nil {
			return b
		}
	case RecorderCmdDigestTest:
		b := &RecorderDigestTestRequest{}
		if b.Decode(data) == nil {
			return b
		}
	}
	return nil
}

// decodeRecorderResponseBlock decodes the GB/T 19056 data block of a terminal
// response (0x0700) into a typed struct. The 0x0700 command byte echoes the
// platform command, so request command values are mapped to response formats.
// Unknown commands return nil.
func decodeRecorderResponseBlock(command uint8, data []byte) any {
	switch command {
	case RecorderCmdCollectInfo:
		b := &RecorderInfoResponse{}
		if b.Decode(data) == nil {
			return b
		}
	case RecorderCmdCollectFile:
		b := &RecorderFileResponse{}
		if b.Decode(data) == nil {
			return b
		}
	case RecorderCmdParamSet:
		b := &RecorderParamSetting{}
		if b.Decode(data) == nil {
			return b
		}
	case RecorderCmdPositionTestOn, RecorderCmdPositionTestOff:
		b := &RecorderPositionTestResponse{}
		if b.Decode(data) == nil {
			return b
		}
	case RecorderCmdDigestTest:
		b := &RecorderDigestTestResponse{}
		if b.Decode(data) == nil {
			return b
		}
	}
	return nil
}

// encodeRecorderBlock returns the encoded bytes of a typed data block. If the
// block is nil or has a nil pointer, the raw fallback data is used.
func encodeRecorderBlock(block any, fallback []byte) ([]byte, error) {
	switch b := block.(type) {
	case *RecorderInfoRequest:
		return b.Encode()
	case *RecorderInfoResponse:
		return b.Encode()
	case *RecorderFileRequest:
		return b.Encode()
	case *RecorderFileResponse:
		return b.Encode()
	case *RecorderParamSetting:
		return b.Encode()
	case *RecorderPositionTestRequest:
		return b.Encode()
	case *RecorderPositionTestResponse:
		return b.Encode()
	case *RecorderDigestTestRequest:
		return b.Encode()
	case *RecorderDigestTestResponse:
		return b.Encode()
	}
	return append([]byte(nil), fallback...), nil
}
