package model

import (
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// JTT1078 查询资源列表
type Msg9205 struct {
	Header         *MsgHeader `json:"header"`
	LogicChannelID uint8      `json:"logicChannelId"` // 逻辑通道号
	StartTime      *time.Time `json:"startTime"`      // 开始时间
	EndTime        *time.Time `json:"endTime"`        // 结束时间
}

func (m *Msg9205) Decode(packet *PacketData) error {
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	m.LogicChannelID = hex.ReadByte(pkt, &idx)
	m.StartTime = hex.ReadTime(pkt, &idx)
	m.EndTime = hex.ReadTime(pkt, &idx)
	return nil
}

func (m *Msg9205) Encode() (pkt []byte, err error) {
	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg9205) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg9205) GenOutgoing(incoming JT808Msg) error {
	return nil
}
