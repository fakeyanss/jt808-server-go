package model

import "github.com/fakeyanss/jt808-server-go/internal/codec/hex"

// 多媒体数据上传应答
type Msg8800 struct {
	Header        *MsgHeader `json:"header"`
	MultiMediaID  uint32     `json:"multiMediaId"`
	RetransmitIDs []uint16   `json:"retransmitIds,omitempty"`
}

func (m *Msg8800) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) < 4 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	i := 0
	m.MultiMediaID = hex.ReadDoubleWord(packet.Body, &i)
	if m.MultiMediaID == 0 {
		return ErrDecodeMsg
	}
	if i == len(packet.Body) {
		return nil
	}
	count := int(hex.ReadByte(packet.Body, &i))
	if count > 125 || len(packet.Body) != i+count*2 {
		return ErrDecodeMsg
	}
	m.RetransmitIDs = make([]uint16, 0, count)
	for n := 0; n < count; n++ {
		m.RetransmitIDs = append(m.RetransmitIDs, hex.ReadWord(packet.Body, &i))
	}
	return nil
}

func (m *Msg8800) Encode() (pkt []byte, err error) {
	if m.Header == nil || m.MultiMediaID == 0 {
		return nil, ErrEncodeMsg
	}
	pkt = hex.WriteDoubleWord(pkt, m.MultiMediaID)
	if len(m.RetransmitIDs) > 0 {
		if len(m.RetransmitIDs) > 125 {
			return nil, ErrEncodeMsg
		}
		pkt = append(pkt, byte(len(m.RetransmitIDs)))
		for _, id := range m.RetransmitIDs {
			pkt = hex.WriteWord(pkt, id)
		}
	}
	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg8800) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg8800) GenOutgoing(incoming JT808Msg) error {
	in, ok := incoming.(*Msg0801)
	if !ok {
		return ErrGenOutgoingMsg
	}
	m.Header = cloneReplyHeader(in.Header, 0x8800)
	m.MultiMediaID = in.MultiMediaID
	return nil
}
