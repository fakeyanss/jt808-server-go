package media

import (
	"encoding/binary"
	"fmt"
)

// JT/T 1078 RTP 音视频包的数据类型与分包类型。
const (
	DataTypeIFrame      uint8 = 0 // 视频 I 帧
	DataTypePFrame      uint8 = 1 // 视频 P 帧
	DataTypeBFrame      uint8 = 2 // 视频 B 帧
	DataTypeAudio       uint8 = 3 // 音频帧
	DataTypePassthrough uint8 = 4 // 透传数据

	SubpackageAtomic uint8 = 0 // 原子帧
	SubpackageFirst  uint8 = 1 // 第一包
	SubpackageLast   uint8 = 2 // 最后一包
	SubpackageMiddle uint8 = 3 // 中间包
)

// PayloadTypeH264 是 1078 中 H.264 的 RTP 载荷类型。
const PayloadTypeH264 uint8 = 98

// jt1078Magic 是 1078 RTP 包的固定 4 字节标识 0x30 0x31 0x63 0x64。
var jt1078Magic = [4]byte{0x30, 0x31, 0x63, 0x64}

// Packet 是解析后的 1078 RTP 包。
//
// 头部长度随数据类型变化（依据 SmallChi JT1078 与 go-jt808 的解析实现）：
//   - 视频（I/P/B）：30 字节，含 8 字节时间戳与 2+2 字节帧间隔；
//   - 音频：26 字节，含 8 字节时间戳；
//   - 透传：18 字节，不含时间戳与帧间隔。
type Packet struct {
	Seq                uint16 // RTP 序列号
	PayloadType        uint8  // M/PT 字节中的载荷类型
	Phone              string // 终端手机号（SIM 卡号 BCD 解码）
	LogicChannel       uint8  // 逻辑通道号
	DataType           uint8  // 数据类型
	SubpackageType     uint8  // 分包类型
	Timestamp          uint64 // 视频/音频时间戳（毫秒）
	LastIFrameInterval uint16 // 与上一 I 帧间隔（毫秒），仅视频
	LastFrameInterval  uint16 // 与上一帧间隔（毫秒），仅视频
	Body               []byte // 载荷
}

// ParsePacket 解析一个 1078 RTP 包。
func ParsePacket(data []byte) (*Packet, error) {
	if len(data) < 18 {
		return nil, fmt.Errorf("jt1078 packet too short: %d bytes", len(data))
	}
	if data[0] != jt1078Magic[0] || data[1] != jt1078Magic[1] ||
		data[2] != jt1078Magic[2] || data[3] != jt1078Magic[3] {
		return nil, fmt.Errorf("not a jt1078 packet: magic %x", data[:4])
	}
	// 1078 实际抓包中版本位为 1（0x41），go-jt808 也不校验版本；
	// 同时容忍标准 RTP 的版本 2。
	version := data[4] >> 6
	if version != 1 && version != 2 {
		return nil, fmt.Errorf("unsupported jt1078 rtp version: %d", version)
	}

	dataType := data[15] >> 4
	subpackageType := data[15] & 0x0f

	// 计算头部长度与载荷长度字段位置。
	var (
		headerLength  int
		bodyLenOffset int
		timestamp     uint64
		iFrameGap     uint16
		frameGap      uint16
	)
	switch dataType {
	case DataTypeIFrame, DataTypePFrame, DataTypeBFrame:
		headerLength, bodyLenOffset = 30, 28
		if len(data) < headerLength {
			return nil, fmt.Errorf("jt1078 video packet too short: %d bytes", len(data))
		}
		timestamp = binary.BigEndian.Uint64(data[16:24])
		iFrameGap = binary.BigEndian.Uint16(data[24:26])
		frameGap = binary.BigEndian.Uint16(data[26:28])
	case DataTypeAudio:
		headerLength, bodyLenOffset = 26, 24
		if len(data) < headerLength {
			return nil, fmt.Errorf("jt1078 audio packet too short: %d bytes", len(data))
		}
		timestamp = binary.BigEndian.Uint64(data[16:24])
	case DataTypePassthrough:
		headerLength, bodyLenOffset = 18, 16
	default:
		return nil, fmt.Errorf("unsupported jt1078 data type: %d", dataType)
	}

	bodyLength := int(binary.BigEndian.Uint16(data[bodyLenOffset : bodyLenOffset+2]))
	if headerLength+bodyLength > len(data) {
		return nil, fmt.Errorf("jt1078 body length %d exceeds packet length %d", bodyLength, len(data))
	}

	phone, err := decodeSIM(data[8:14])
	if err != nil {
		return nil, err
	}

	return &Packet{
		Seq:                binary.BigEndian.Uint16(data[6:8]),
		PayloadType:        data[5] & 0x7f,
		Phone:              phone,
		LogicChannel:       data[14],
		DataType:           dataType,
		SubpackageType:     subpackageType,
		Timestamp:          timestamp,
		LastIFrameInterval: iFrameGap,
		LastFrameInterval:  frameGap,
		Body:               data[headerLength : headerLength+bodyLength],
	}, nil
}

// decodeSIM 把 6 字节 BCD 终端手机号解码为 12 位数字。
func decodeSIM(bcd []byte) (string, error) {
	out := make([]byte, 0, len(bcd)*2)
	for _, b := range bcd {
		hi, lo := b>>4, b&0x0f
		if hi > 9 || lo > 9 {
			return "", fmt.Errorf("invalid jt1078 sim bcd: %x", bcd)
		}
		out = append(out, '0'+hi, '0'+lo)
	}
	return string(out), nil
}

// Frame 组装器把分包的 RTP 包还原成完整帧。

// frameAssembler 按数据类型分别缓存不完整的分片帧。
type frameAssembler struct {
	buffers  map[uint8][]byte
	maxBytes int
}

func newFrameAssembler(maxBytes int) *frameAssembler {
	return &frameAssembler{buffers: map[uint8][]byte{}, maxBytes: maxBytes}
}

// Push 接收一个分片包；返回完整帧载荷，未收齐时返回 nil。
func (a *frameAssembler) Push(pkt *Packet) []byte {
	body := pkt.Body
	switch pkt.SubpackageType {
	case SubpackageAtomic:
		return body
	case SubpackageFirst:
		a.buffers[pkt.DataType] = append([]byte(nil), body...)
		return nil
	case SubpackageMiddle:
		buf, ok := a.buffers[pkt.DataType]
		if !ok {
			return nil
		}
		if len(buf)+len(body) > a.maxBytes {
			delete(a.buffers, pkt.DataType)
			return nil
		}
		a.buffers[pkt.DataType] = append(buf, body...)
		return nil
	case SubpackageLast:
		buf, ok := a.buffers[pkt.DataType]
		if !ok {
			return nil
		}
		delete(a.buffers, pkt.DataType)
		if len(buf)+len(body) > a.maxBytes {
			return nil
		}
		return append(buf, body...)
	default:
		return nil
	}
}
