package model

import (
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// JT/T 1078-2016 实时音视频传输状态通知（0x9105）
//
// 终端通过该消息上报当前实时音视频传输通道的丢包率。
// 注：任务描述中的"实时音视频流及透传数据传输"与两份参考实现
// （SmallChi JT1078 扩展、go-jt808）以及协议消息表不一致，
// 此处按双源一致的"传输状态通知"字段实现。
type Msg9105 struct {
	Header         *MsgHeader `json:"header"`
	LogicChannelID uint8      `json:"logicChannelId"` // 逻辑通道号
	PacketLossRate uint8      `json:"packetLossRate"` // 丢包率，数值乘以 100 后取整部分
}

func (m *Msg9105) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) != 2 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	m.LogicChannelID = hex.ReadByte(pkt, &idx)
	m.PacketLossRate = hex.ReadByte(pkt, &idx)
	return nil
}

func (m *Msg9105) Encode() (pkt []byte, err error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt = hex.WriteByte(pkt, m.LogicChannelID)
	pkt = hex.WriteByte(pkt, m.PacketLossRate)
	return writeHeader(m, pkt)
}

func (m *Msg9105) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg9105) GenOutgoing(_ JT808Msg) error {
	return nil
}
