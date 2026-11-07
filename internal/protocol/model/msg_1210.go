package model

import (
	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// 苏标 T/JSATL 12-2017 4.6.2 报警附件信息消息（0x1210）
//
// 终端根据平台下发的 0x9208 附件上传指令连接附件服务器后上报本消息，
// 登记本次报警关联的附件清单。附件清单只包含文件名与文件大小，
// 每个附件的类型由 0x1211 文件信息上传单独声明。
type Msg1210 struct {
	Header          *MsgHeader             `json:"header"`
	TerminalID      string                 `json:"terminalId"`      // 终端ID，7字节，大写字母和数字，不足补 0x00
	AlarmSign       []byte                 `json:"alarmSign"`       // 报警标识号，16字节
	AlarmNo         string                 `json:"alarmNo"`         // 报警编号，平台分配的唯一编号，32字节
	InfoType        uint8                  `json:"infoType"`        // 0x00 正常报警文件信息；0x01 补传报警文件信息
	AttachmentCount uint8                  `json:"attachmentCount"` // 附件数量
	Attachments     []AlarmAttachmentEntry `json:"attachments"`     // 附件信息列表
}

// AlarmAttachmentEntry 是 0x1210 附件信息列表中的一项（表 4-24）。
type AlarmAttachmentEntry struct {
	FileName string `json:"fileName"`
	FileSize uint32 `json:"fileSize"`
}

func (m *Msg1210) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	pkt, idx := packet.Body, 0
	const fixedLen = 7 + 16 + 32 + 1 + 1
	if len(pkt) < fixedLen {
		return ErrDecodeMsg
	}
	m.TerminalID = trimNUL(hex.ReadString(pkt, &idx, 7))
	m.AlarmSign = append([]byte(nil), hex.ReadBytes(pkt, &idx, 16)...)
	m.AlarmNo = trimNUL(hex.ReadString(pkt, &idx, 32))
	m.InfoType = hex.ReadByte(pkt, &idx)
	m.AttachmentCount = hex.ReadByte(pkt, &idx)
	m.Attachments = nil
	for n := uint8(0); n < m.AttachmentCount; n++ {
		if idx >= len(pkt) {
			return ErrDecodeMsg
		}
		nameLen := int(hex.ReadByte(pkt, &idx))
		if nameLen == 0 || idx+nameLen+4 > len(pkt) {
			return ErrDecodeMsg
		}
		entry := AlarmAttachmentEntry{FileName: hex.ReadString(pkt, &idx, nameLen)}
		entry.FileSize = hex.ReadDoubleWord(pkt, &idx)
		m.Attachments = append(m.Attachments, entry)
	}
	return nil
}

func (m *Msg1210) Encode() (pkt []byte, err error) {
	if m.Header == nil {
		return nil, ErrEncodeMsg
	}
	terminalID := m.TerminalID
	if len(terminalID) > 7 {
		terminalID = terminalID[:7]
	}
	for len(terminalID) < 7 {
		terminalID += "\x00"
	}
	alarmSign := padOrTrim(m.AlarmSign, 16)
	alarmNo := padOrTrim([]byte(m.AlarmNo), 32)

	pkt = hex.WriteString(pkt, terminalID)
	pkt = hex.WriteBytes(pkt, alarmSign)
	pkt = hex.WriteBytes(pkt, alarmNo)
	pkt = hex.WriteByte(pkt, m.InfoType)
	pkt = hex.WriteByte(pkt, uint8(len(m.Attachments)))
	for _, entry := range m.Attachments {
		name := []byte(entry.FileName)
		if len(name) > 255 {
			name = name[:255]
		}
		pkt = hex.WriteByte(pkt, uint8(len(name)))
		pkt = hex.WriteBytes(pkt, name)
		pkt = hex.WriteDoubleWord(pkt, entry.FileSize)
	}
	return writeHeader(m, pkt)
}

func (m *Msg1210) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg1210) GenOutgoing(_ JT808Msg) error {
	return ErrGenOutgoingMsg
}

// padOrTrim 把字节序列补齐或截断到固定长度，用于协议中的固定长度字段。
func padOrTrim(src []byte, n int) []byte {
	dst := make([]byte, n)
	copy(dst, src)
	return dst
}
