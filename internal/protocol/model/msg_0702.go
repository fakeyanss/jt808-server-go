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
