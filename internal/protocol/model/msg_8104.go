package model

type Msg8104 struct {
	Header *MsgHeader `json:"header"`
}

func (m *Msg8104) Decode(packet *PacketData) error {
	m.Header = packet.Header
	return nil
}

func (m *Msg8104) Encode() (pkt []byte, err error) {
	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg8104) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg8104) GenOutgoing(incoming JT808Msg) error {
	// todo: client, gen Msg0104
	return nil
}
