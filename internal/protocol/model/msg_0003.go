package model

// 终端注销
type Msg0003 struct {
	Header *MsgHeader `json:"header"`
	// 消息体为空
}

func (m *Msg0003) Decode(packet *PacketData) error {
	m.Header = packet.Header
	return nil
}

func (m *Msg0003) Encode() (pkt []byte, err error) {
	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg0003) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0003) GenOutgoing(incoming JT808Msg) error {
	return nil
}
