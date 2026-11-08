package model

import (
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// JT/T 1078-2016 文件上传指令（0x9206）
//
// 平台下发 FTP 服务器参数与待上传文件的时间范围，终端完成后以 0x1206 通知平台。
// 注：任务描述把 0x9206 称为"文件上传完成通知"，与两份参考实现
// （SmallChi JT1078 扩展、go-jt808）的消息表不一致，此处按双源一致定义实现。
type Msg9206 struct {
	Header           *MsgHeader `json:"header"`
	ServerIPLength   uint8      `json:"serverIpLength"`   // 服务器 IP 地址长度
	ServerIP         string     `json:"serverIp"`         // 服务器 IP 地址
	Port             uint16     `json:"port"`             // 服务器端口
	UsernameLength   uint8      `json:"usernameLength"`   // 用户名长度
	Username         string     `json:"username"`         // 用户名
	PasswordLength   uint8      `json:"passwordLength"`   // 密码长度
	Password         string     `json:"password"`         // 密码
	UploadPathLength uint8      `json:"uploadPathLength"` // 文件上传路径长度
	UploadPath       string     `json:"uploadPath"`       // 文件上传路径
	LogicChannelID   uint8      `json:"logicChannelId"`   // 逻辑通道号
	StartTime        *time.Time `json:"startTime"`        // 起始时间
	EndTime          *time.Time `json:"endTime"`          // 结束时间
	AlarmSign        uint64     `json:"alarmSign"`        // 报警标志
	MediaType        uint8      `json:"mediaType"`        // 音视频类型。0 音视频；1 音频；2 视频；3 视频或音视频
	StreamType       uint8      `json:"streamType"`       // 码流类型。0 所有码流；1 主码流；2 子码流
	StorageType      uint8      `json:"storageType"`      // 存储器类型。0 所有存储器；1 主存储器；2 灾备存储器
	TaskCondition    uint8      `json:"taskCondition"`    // 任务执行条件，bit0 WIFI；bit1 LAN；bit2 3G/4G
}

func (m *Msg9206) Decode(packet *PacketData) error {
	if packet == nil || packet.Header == nil || len(packet.Body) < 1 {
		return ErrDecodeMsg
	}
	pkt, idx := packet.Body, 0
	n := int(hex.ReadByte(pkt, &idx))
	if idx+n+2 > len(pkt) {
		return ErrDecodeMsg
	}
	m.Header = packet.Header
	m.ServerIPLength = uint8(n)
	m.ServerIP = hex.ReadString(pkt, &idx, n)
	m.Port = hex.ReadWord(pkt, &idx)

	username, ok := readLengthPrefixed(pkt, &idx)
	if !ok {
		return ErrDecodeMsg
	}
	m.UsernameLength = uint8(len(username))
	m.Username = username

	password, ok := readLengthPrefixed(pkt, &idx)
	if !ok {
		return ErrDecodeMsg
	}
	m.PasswordLength = uint8(len(password))
	m.Password = password

	uploadPath, ok := readLengthPrefixed(pkt, &idx)
	if !ok {
		return ErrDecodeMsg
	}
	m.UploadPathLength = uint8(len(uploadPath))
	m.UploadPath = uploadPath

	// 通道号 1 + 起止时间 12 + 报警标志 8 + 类型/条件 4
	if idx+25 != len(pkt) {
		return ErrDecodeMsg
	}
	m.LogicChannelID = hex.ReadByte(pkt, &idx)
	m.StartTime = readOptionalTime(pkt, &idx)
	m.EndTime = readOptionalTime(pkt, &idx)
	m.AlarmSign = uint64(hex.ReadDoubleWord(pkt, &idx))<<32 | uint64(hex.ReadDoubleWord(pkt, &idx))
	m.MediaType = hex.ReadByte(pkt, &idx)
	m.StreamType = hex.ReadByte(pkt, &idx)
	m.StorageType = hex.ReadByte(pkt, &idx)
	m.TaskCondition = hex.ReadByte(pkt, &idx)
	return nil
}

func (m *Msg9206) Encode() (pkt []byte, err error) {
	if m.Header == nil || len(m.ServerIP) > 255 || len(m.Username) > 255 || len(m.Password) > 255 || len(m.UploadPath) > 255 {
		return nil, ErrEncodeMsg
	}
	m.ServerIPLength = uint8(len(m.ServerIP))
	m.UsernameLength = uint8(len(m.Username))
	m.PasswordLength = uint8(len(m.Password))
	m.UploadPathLength = uint8(len(m.UploadPath))
	pkt = hex.WriteByte(pkt, m.ServerIPLength)
	pkt = hex.WriteString(pkt, m.ServerIP)
	pkt = hex.WriteWord(pkt, m.Port)
	pkt = writeLengthPrefixed(pkt, m.Username, m.UsernameLength)
	pkt = writeLengthPrefixed(pkt, m.Password, m.PasswordLength)
	pkt = writeLengthPrefixed(pkt, m.UploadPath, m.UploadPathLength)
	pkt = hex.WriteByte(pkt, m.LogicChannelID)
	pkt = writeOptionalTime(pkt, m.StartTime)
	pkt = writeOptionalTime(pkt, m.EndTime)
	pkt = hex.WriteDoubleWord(pkt, uint32(m.AlarmSign>>32))
	pkt = hex.WriteDoubleWord(pkt, uint32(m.AlarmSign))
	pkt = hex.WriteByte(pkt, m.MediaType)
	pkt = hex.WriteByte(pkt, m.StreamType)
	pkt = hex.WriteByte(pkt, m.StorageType)
	pkt = hex.WriteByte(pkt, m.TaskCondition)
	return writeHeader(m, pkt)
}

func (m *Msg9206) GetHeader() *MsgHeader {
	return m.Header
}

func (m *Msg9206) GenOutgoing(_ JT808Msg) error {
	return nil
}

// readLengthPrefixed 读取“长度 1 字节 + 变长内容”字段。
func readLengthPrefixed(pkt []byte, idx *int) (string, bool) {
	if *idx >= len(pkt) {
		return "", false
	}
	n := int(hex.ReadByte(pkt, idx))
	if *idx+n > len(pkt) {
		return "", false
	}
	return hex.ReadString(pkt, idx, n), true
}

func writeLengthPrefixed(pkt []byte, value string, length uint8) []byte {
	pkt = hex.WriteByte(pkt, length)
	return hex.WriteString(pkt, value)
}
