package model

// JT1078 查询资源列表
type Msg9205 struct {
	Header *MsgHeader `json:"header"`
	DeviceMediaQuery
}

func (m *Msg9205) Decode(packet *PacketData) error {
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	m.DeviceMediaQuery.Decode(pkt, &idx)
	return nil
}

func (m *Msg9205) Encode() (pkt []byte, err error) {
	pkt = m.DeviceMediaQuery.Encode()

	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg9205) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg9205) GenOutgoing(_ JT808Msg) error {
	return nil
}
