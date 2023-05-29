package model

import (
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

type DeviceMediaQuery struct {
	LogicChannelID uint8      `json:"logicChannelId"` // 逻辑通道号
	StartTime      *time.Time `json:"startTime"`      // 开始时间
	EndTime        *time.Time `json:"endTime"`        // 结束时间
	AlarmSign      uint32     `json:"alarmSign"`      // 报警标志位。bit0-bit31为0x0200的报警标志位，
	AlarmSignExt   uint32     `json:"alarmSignExt"`   // 报警标志位。bit32-bit63？，全0表示无报警类型条件
	MediaType      uint8      `json:"mediaType"`      // 音视频类型。0：音视频；1：音频；2：视频；3：视频或音视频
	StreamType     uint8      `json:"streamType"`     // 码流类型。0：所有码流；1：主码流；2：子码流
	StorageType    uint8      `json:"storageType"`    // 存储器类型。0：所有存储器；1：主存储器；2：灾备存储器
}

func (q *DeviceMediaQuery) Decode(pkt []byte, idx *int) {
	q.LogicChannelID = hex.ReadByte(pkt, idx)
	q.StartTime = hex.ReadTime(pkt, idx)
	q.EndTime = hex.ReadTime(pkt, idx)
	q.AlarmSign = hex.ReadDoubleWord(pkt, idx)
	q.AlarmSignExt = hex.ReadDoubleWord(pkt, idx)
	q.MediaType = hex.ReadByte(pkt, idx)
	q.StreamType = hex.ReadByte(pkt, idx)
	q.StorageType = hex.ReadByte(pkt, idx)
}

func (q *DeviceMediaQuery) Encode() (pkt []byte) {
	pkt = hex.WriteByte(pkt, q.LogicChannelID)
	pkt = hex.WriteTime(pkt, *q.StartTime)
	pkt = hex.WriteTime(pkt, *q.EndTime)
	pkt = hex.WriteDoubleWord(pkt, q.AlarmSign)
	pkt = hex.WriteDoubleWord(pkt, q.AlarmSignExt)
	pkt = hex.WriteByte(pkt, q.MediaType)
	pkt = hex.WriteByte(pkt, q.StreamType)
	pkt = hex.WriteByte(pkt, q.StorageType)
	return pkt
}

type DeviceMedia struct {
	DeviceMediaQuery
	Size uint32 // 文件大小，单位Byte
}

func (m *DeviceMedia) Decode(pkt []byte) {
	idx := 0
	m.DeviceMediaQuery.Decode(pkt, &idx)
	m.Size = hex.ReadDoubleWord(pkt, &idx)
}

func (m *DeviceMedia) Encode() (pkt []byte) {
	pkt = m.DeviceMediaQuery.Encode()
	pkt = hex.WriteDoubleWord(pkt, m.Size)
	return pkt
}
