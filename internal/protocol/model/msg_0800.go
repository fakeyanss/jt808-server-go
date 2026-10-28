package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

type Msg0800 struct {
	Header              *MsgHeader `json:"header"`
	MultiMediaID        uint32     `json:"multiMediaId"`
	MultiMediaType      uint8      `json:"multiMediaType"`
	MultiMediaContainer uint8      `json:"multiMediaContainer"`
	EventID             uint8      `json:"eventId"`
	LogicChannelID      uint8      `json:"logicChannelId"`
}

func (m *Msg0800) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) != 8 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	i := 0
	m.MultiMediaID = hex.ReadDoubleWord(packet.Body, &i)
	if m.MultiMediaID == 0 {
		return ErrDecodeMsg
	}
	m.MultiMediaType = hex.ReadByte(packet.Body, &i)
	m.MultiMediaContainer = hex.ReadByte(packet.Body, &i)
	m.EventID = hex.ReadByte(packet.Body, &i)
	m.LogicChannelID = hex.ReadByte(packet.Body, &i)
	return nil
}

func (m *Msg0800) Encode() (pkt []byte, err error) {
	if m.Header == nil || m.MultiMediaID == 0 {
		return nil, ErrEncodeMsg
	}
	pkt = hex.WriteDoubleWord(pkt, m.MultiMediaID)
	pkt = append(pkt, m.MultiMediaType, m.MultiMediaContainer, m.EventID, m.LogicChannelID)
	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg0800) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0800) GenOutgoing(_ JT808Msg) error {
	return nil
}
