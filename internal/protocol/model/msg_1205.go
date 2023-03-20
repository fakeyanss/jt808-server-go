package model

import (
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// JTT1078 终端上传音视频资源列表
//
// 列表过大时需要分包
type Msg1205 struct {
	Header             *MsgHeader `json:"header"`
	AnswerSerialNumber uint16     `json:"answerSerialNumber"` // 流水号，对应查询音视频资源列表消息的流水号
	MediaCount         uint32     `json:"mediaCount"`         // 音视频资源总数
	DeviceMedia
}

func (m *Msg1205) Decode(packet *PacketData) error {
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	m.AnswerSerialNumber = hex.ReadWord(pkt, &idx)
	m.MediaCount = hex.ReadDoubleWord(pkt, &idx)
	m.DeviceMedia.Decode(pkt[idx:])
	return nil
}

func (m *Msg1205) Encode() (pkt []byte, err error) {
	pkt = hex.WriteWord(pkt, m.AnswerSerialNumber)
	pkt = hex.WriteDoubleWord(pkt, m.MediaCount)
	pkt = hex.WriteBytes(pkt, m.DeviceMedia.Encode())

	pkt, err = writeHeader(m, pkt)
	return pkt, err
}

func (m *Msg1205) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg1205) GenOutgoing(incoming JT808Msg) error {
	in, ok := incoming.(*Msg9205)
	if !ok {
		return ErrGenOutgoingMsg
	}
	m.AnswerSerialNumber = in.Header.SerialNumber
	m.Header = in.Header
	m.Header.MsgID = 0x1205

	return nil
}
