package model

// 多媒体数据上传应答
type Msg8800 struct {
	Header *MsgHeader `json:"header"`
}

func (m *Msg8800) Decode(packet *PacketData) error {
	m.Header = packet.Header
	return nil
}

func (m *Msg8800) Encode() (pkt []byte, err error) {
	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg8800) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg8800) GenOutgoing(incoming JT808Msg) error {
	return nil
}
