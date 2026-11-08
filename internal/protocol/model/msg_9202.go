package model

import (
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// JT/T 1078-2016 平台下发远程录像回放控制（0x9202）
type Msg9202 struct {
	Header         *MsgHeader `json:"header"`
	LogicChannelID uint8      `json:"logicChannelId"` // 音视频通道号
	PlayControl    uint8      `json:"playControl"`    // 回放控制。0 开始；1 暂停；2 结束；3 快进；4 关键帧快退；5 拖动；6 关键帧播放
	PlaySpeed      uint8      `json:"playSpeed"`      // 快进或快退倍数，PlayControl 为 3/4 时有效
	DragPosition   *time.Time `json:"dragPosition"`   // 拖动回放位置，PlayControl 为 5 时有效
}

func (m *Msg9202) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) != 9 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	m.LogicChannelID = hex.ReadByte(pkt, &idx)
	m.PlayControl = hex.ReadByte(pkt, &idx)
	m.PlaySpeed = hex.ReadByte(pkt, &idx)
	m.DragPosition = readOptionalTime(pkt, &idx)
	return nil
}

func (m *Msg9202) Encode() (pkt []byte, err error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt = hex.WriteByte(pkt, m.LogicChannelID)
	pkt = hex.WriteByte(pkt, m.PlayControl)
	pkt = hex.WriteByte(pkt, m.PlaySpeed)
	pkt = writeOptionalTime(pkt, m.DragPosition)
	return writeHeader(m, pkt)
}

func (m *Msg9202) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg9202) GenOutgoing(_ JT808Msg) error {
	return nil
}
