package model

import (
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// JT/T 1078-2016 音视频实时传输控制（0x9102）
//
// 平台通过该消息控制终端的实时音视频传输。
type Msg9102 struct {
	Header         *MsgHeader `json:"header"`
	LogicChannelID uint8      `json:"logicChannelId"` // 逻辑通道号
	ControlCmd     uint8      `json:"controlCmd"`     // 控制指令。0 关闭音视频传输；1 切换码流；2 暂停该通道所有流；3 恢复暂停前流；4 关闭双向对讲
	CloseMediaType uint8      `json:"closeMediaType"` // 关闭音视频类型。0 音视频；1 只关闭音频；2 只关闭视频
	StreamType     uint8      `json:"streamType"`     // 切换码流类型。0 主码流；1 子码流
}

func (m *Msg9102) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) != 4 {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	m.LogicChannelID = hex.ReadByte(pkt, &idx)
	m.ControlCmd = hex.ReadByte(pkt, &idx)
	m.CloseMediaType = hex.ReadByte(pkt, &idx)
	m.StreamType = hex.ReadByte(pkt, &idx)
	return nil
}

func (m *Msg9102) Encode() (pkt []byte, err error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	pkt = hex.WriteByte(pkt, m.LogicChannelID)
	pkt = hex.WriteByte(pkt, m.ControlCmd)
	pkt = hex.WriteByte(pkt, m.CloseMediaType)
	pkt = hex.WriteByte(pkt, m.StreamType)
	return writeHeader(m, pkt)
}

func (m *Msg9102) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg9102) GenOutgoing(_ JT808Msg) error {
	return nil
}
