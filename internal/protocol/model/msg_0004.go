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
	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg0004) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0004) GenOutgoing(_ JT808Msg) error {
	return nil
}
