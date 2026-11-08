package model

import (
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// JT/T 1078-2016 平台下发远程录像回放请求（0x9201）
//
// 在实时音视频请求字段基础上追加存储器类型、回放方式、回放倍数与起止时间。
// 时间字段为 BCD[6]（YY-MM-DD-HH-MM-SS）。
type Msg9201 struct {
	Header         *MsgHeader `json:"header"`
	ServerIPLength uint8      `json:"serverIpLength"` // 视频服务器 IP 地址长度
	ServerIP       string     `json:"serverIp"`       // 视频服务器 IP 地址
	TCPPort        uint16     `json:"tcpPort"`        // 视频服务器 TCP 端口号
	UDPPort        uint16     `json:"udpPort"`        // 视频服务器 UDP 端口号
	LogicChannelID uint8      `json:"logicChannelId"` // 逻辑通道号
	MediaType      uint8      `json:"mediaType"`      // 音视频类型。0 音视频；1 音频；2 视频；3 视频或音视频
	StreamType     uint8      `json:"streamType"`     // 码流类型。0 所有码流；1 主码流；2 子码流
	StorageType    uint8      `json:"storageType"`    // 存储器类型。0 所有存储器；1 主存储器；2 灾备存储器
	PlaybackMode   uint8      `json:"playbackMode"`   // 回放方式。0 正常；1 快进；2 关键帧快退；3 关键帧播放；4 单帧上传
	PlaybackSpeed  uint8      `json:"playbackSpeed"`  // 快进或快退倍数。0 无效；1 1倍；2 2倍；3 4倍；4 8倍；5 16倍
	StartTime      *time.Time `json:"startTime"`      // 开始时间，单帧上传时表示上传时间
	EndTime        *time.Time `json:"endTime"`        // 结束时间
}

func (m *Msg9201) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) < 1 {
		return ErrDecodeMsg
	}
	pkt, idx := packet.Body, 0
	n := int(hex.ReadByte(pkt, &idx))
	if idx+n+22 != len(pkt) {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	m.ServerIPLength = uint8(n)
	m.ServerIP = hex.ReadString(pkt, &idx, n)
	m.TCPPort = hex.ReadWord(pkt, &idx)
	m.UDPPort = hex.ReadWord(pkt, &idx)
	m.LogicChannelID = hex.ReadByte(pkt, &idx)
	m.MediaType = hex.ReadByte(pkt, &idx)
	m.StreamType = hex.ReadByte(pkt, &idx)
	m.StorageType = hex.ReadByte(pkt, &idx)
	m.PlaybackMode = hex.ReadByte(pkt, &idx)
	m.PlaybackSpeed = hex.ReadByte(pkt, &idx)
	m.StartTime = readOptionalTime(pkt, &idx)
	m.EndTime = readOptionalTime(pkt, &idx)
	return nil
}

func (m *Msg9201) Encode() (pkt []byte, err error) {
	if m.Header == nil || len(m.ServerIP) > 255 {
		return nil, ErrEncodeMsg
	}
	m.ServerIPLength = uint8(len(m.ServerIP))
	pkt = hex.WriteByte(pkt, m.ServerIPLength)
	pkt = hex.WriteString(pkt, m.ServerIP)
	pkt = hex.WriteWord(pkt, m.TCPPort)
	pkt = hex.WriteWord(pkt, m.UDPPort)
	pkt = hex.WriteByte(pkt, m.LogicChannelID)
	pkt = hex.WriteByte(pkt, m.MediaType)
	pkt = hex.WriteByte(pkt, m.StreamType)
	pkt = hex.WriteByte(pkt, m.StorageType)
	pkt = hex.WriteByte(pkt, m.PlaybackMode)
	pkt = hex.WriteByte(pkt, m.PlaybackSpeed)
	pkt = writeOptionalTime(pkt, m.StartTime)
	pkt = writeOptionalTime(pkt, m.EndTime)
	return writeHeader(m, pkt)
}

func (m *Msg9201) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg9201) GenOutgoing(_ JT808Msg) error {
	return nil
}

// writeOptionalTime 编码 6 字节 BCD 时间；nil 表示全零占位。
func writeOptionalTime(pkt []byte, t *time.Time) []byte {
	if t == nil {
		return append(pkt, 0, 0, 0, 0, 0, 0)
	}
	return hex.WriteTime(pkt, *t)
}
