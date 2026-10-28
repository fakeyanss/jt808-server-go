package model

import (
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// Msg8105 is a platform terminal-control command. Command parameters are an
// optional GBK string; the protocol defines their meaning per command word.
type Msg8105 struct {
	Header       *MsgHeader `json:"header"`
	CommandWord  uint8      `json:"commandWord"`
	CommandParam string     `json:"commandParam,omitempty"`
}

func (m *Msg8105) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) < 1 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	m.CommandWord = packet.Body[0]
	if len(packet.Body) > 1 {
		idx := 1
		m.CommandParam = hex.ReadGBK(packet.Body, &idx, len(packet.Body)-1)
	}
	return nil
}

func (m *Msg8105) Encode() ([]byte, error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	payload := []byte{m.CommandWord}
	payload = hex.WriteGBK(payload, m.CommandParam)
	return writeHeader(m, payload)
}

func (m *Msg8105) GetHeader() *MsgHeader { return m.Header }

func (m *Msg8105) GenOutgoing(_ JT808Msg) error { return ErrGenOutgoingMsg }
