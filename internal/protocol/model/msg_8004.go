package model

import (
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// 查询服务器时间应答
type Msg8004 struct {
	Header     *MsgHeader `json:"header"`
	ServerTime *time.Time `json:"serverTime"`
}

func (m *Msg8004) Decode(packet *PacketData) error {
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	m.ServerTime = hex.ReadTime(pkt, &idx)
	return nil
}

func (m *Msg8004) Encode() (pkt []byte, err error) {
	pkt = hex.WriteTime(pkt, *m.ServerTime)

	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg8004) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg8004) GenOutgoing(incoming JT808Msg) error {
	in := incoming.(*Msg0004)
	now := time.Now()
	m.ServerTime = &now

	m.Header = in.Header
	m.Header.MsgID = MsgID8004

	return nil
}
