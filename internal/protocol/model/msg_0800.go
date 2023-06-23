package model

// todo: 多媒体事件消息上传
type Msg0800 struct {
	Header *MsgHeader `json:"header"`
}

func (m *Msg0800) Decode(packet *PacketData) error {
	m.Header = packet.Header
	return nil
}

func (m *Msg0800) Encode() (pkt []byte, err error) {
	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg0800) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0800) GenOutgoing(_ JT808Msg) error {
	return nil
}
