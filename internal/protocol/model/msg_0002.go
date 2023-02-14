package model

// 终端心跳
type Msg0002 struct {
	Header *MsgHeader `json:"header"`
	// 消息体为空
}

func (m *Msg0002) Decode(packet *PacketData) error {
	m.Header = packet.Header
	return nil
}

func (m *Msg0002) Encode() (pkt []byte, err error) {
	headerPkt, err := m.Header.Encode()
	if err != nil {
		return nil, err
	}
	pkt = append(headerPkt, pkt...)
	return pkt, nil
}

func (m *Msg0002) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0002) GenOutgoing(incoming JT808Msg) error {
	return nil
}
