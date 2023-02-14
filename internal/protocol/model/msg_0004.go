package model

// 查询服务器时间请求，2019版消息
type Msg0004 struct {
	Header *MsgHeader `json:"header"`
	// 消息体为空
}

func (m *Msg0004) Decode(packet *PacketData) error {
	m.Header = packet.Header
	return nil
}

func (m *Msg0004) Encode() (pkt []byte, err error) {
	headerPkt, err := m.Header.Encode()
	if err != nil {
		return nil, err
	}
	pkt = append(headerPkt, pkt...)
	return pkt, nil
}

func (m *Msg0004) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0004) GenOutgoing(incoming JT808Msg) error {
	return nil
}
