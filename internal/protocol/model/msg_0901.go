package model

import (
	"bytes"
	"compress/zlib"
	"io"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// Msg0901 is a terminal compressed data report. The Data field holds the raw
// zlib-compressed payload; Decompressed holds the inflated bytes when
// decompression succeeds.
type Msg0901 struct {
	Header       *MsgHeader `json:"header"`
	Length       uint32     `json:"length"`       // 压缩消息长度
	Data         []byte     `json:"data"`         // 压缩消息体
	Decompressed []byte     `json:"decompressed"` // 解压后的消息体（解压失败时为 nil）
}

func (m *Msg0901) Decode(p *PacketData) error {
	if p == nil || p.Header == nil || len(p.Body) < 4 {
		return ErrDecodeMsg
	}
	m.Header = p.Header
	idx := 0
	m.Length = hex.ReadDoubleWord(p.Body, &idx)
	dataLen := int(m.Length)
	if idx+dataLen > len(p.Body) {
		dataLen = len(p.Body) - idx
	}
	m.Data = hex.ReadBytes(p.Body, &idx, dataLen)
	m.Decompressed = decompressZlib(m.Data)
	return nil
}

func (m *Msg0901) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt := hex.WriteDoubleWord(nil, uint32(len(m.Data)))
	pkt = append(pkt, m.Data...)
	return writeHeader(m, pkt)
}

func (m *Msg0901) GetHeader() *MsgHeader { return m.Header }
func (m *Msg0901) GenOutgoing(_ JT808Msg) error {
	return nil
}

// decompressZlib inflates a zlib-compressed payload. It returns nil if the
// input is empty or decompression fails, leaving the original bytes untouched.
func decompressZlib(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return nil
	}
	return out
}
