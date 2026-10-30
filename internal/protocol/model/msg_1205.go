package model

import (
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// JTT1078 终端上传音视频资源列表
//
// 列表过大时需要分包
type Msg1205 struct {
	Header             *MsgHeader    `json:"header"`
	AnswerSerialNumber uint16        `json:"answerSerialNumber"` // 流水号，对应查询音视频资源列表消息的流水号
	MediaCount         uint32        `json:"mediaCount"`         // 音视频资源总数
	Items              []DeviceMedia `json:"items"`              // 资源列表
}

func (m *Msg1205) Decode(packet *PacketData) error {
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	m.AnswerSerialNumber = hex.ReadWord(pkt, &idx)
	m.MediaCount = hex.ReadDoubleWord(pkt, &idx)
	m.Items = nil
	for n := uint32(0); n < m.MediaCount; n++ {
		if idx+28 > len(pkt) {
			return ErrDecodeMsg
		}
		var item DeviceMedia
		item.Decode(pkt[idx : idx+28])
		idx += 28
		m.Items = append(m.Items, item)
	}
	return nil
}

func (m *Msg1205) Encode() (pkt []byte, err error) {
	pkt = hex.WriteWord(pkt, m.AnswerSerialNumber)
	pkt = hex.WriteDoubleWord(pkt, m.MediaCount)
	for _, item := range m.Items {
		pkt = hex.WriteBytes(pkt, item.Encode())
	}

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
