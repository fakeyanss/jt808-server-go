package model

// 多媒体数据上传
type Msg0801 struct {
	Header *MsgHeader `json:"header"`
}

func (m *Msg0801) Decode(packet *PacketData) error {
	m.Header = packet.Header
	return nil
}

func (m *Msg0801) Encode() (pkt []byte, err error) {
	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg0801) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg0801) GenOutgoing(incoming JT808Msg) error {
	return nil
}
