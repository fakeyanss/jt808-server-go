package model

// 查询终端参数
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

func (m *Msg8104) GenOutgoing(_ JT808Msg) error {
	// will not use
	return nil
}
