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

// 音频载荷类型，取值与 JT/T 1078 附录一致。
const (
	PayloadTypeG711A uint8 = 6
	PayloadTypeG711U uint8 = 7
	PayloadTypeAAC   uint8 = 19
)

// 1078 终端手机号字段支持两种宽度：规范定义的 6 字节（12 位 BCD）
// 与部分终端扩展使用的 10 字节（20 位 BCD）。
const (
	simLengthShort = 6
	simLengthLong  = 10
)

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

	consumed int // 头部与载荷的总长度，用于区分 6/10 字节 SIM
}

// ParsePacket 解析一个 1078 RTP 包，兼容 6 字节与 10 字节 SIM。
//
// UDP 的每个 datagram 恰好承载一个包，因此优先按 6 字节 SIM 解析；
// 仅当 6 字节解释无法覆盖整个 datagram、而 10 字节解释恰好覆盖时才采用后者。
func ParsePacket(data []byte) (*Packet, error) {
	pkt, err := ParsePacketWithSIM(data, simLengthShort)
	if err == nil {
		if pkt.consumed == len(data) {
			return pkt, nil
		}
		if long, longErr := ParsePacketWithSIM(data, simLengthLong); longErr == nil && long.consumed == len(data) {
			return long, nil
		}
		return pkt, nil
	}
	long, longErr := ParsePacketWithSIM(data, simLengthLong)
	if longErr == nil && long.consumed == len(data) {
		return long, nil
	}
	return nil, err
}

// ParsePacketWithSIM 按指定的 SIM 字节宽度解析一个 1078 包。
// TCP 分帧器在连接建立时探测宽度后复用该入口。
func ParsePacketWithSIM(data []byte, simLen int) (*Packet, error) {
	if simLen != simLengthShort && simLen != simLengthLong {
		return nil, fmt.Errorf("unsupported jt1078 sim length: %d", simLen)
	}
	minLength := 4 + 2 + 2 + simLen + 1 + 1
	if len(data) < minLength {
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

	channelOffset := 8 + simLen
	flagOffset := channelOffset + 1
	dataType := data[flagOffset] >> 4
	subpackageType := data[flagOffset] & 0x0f

	headerLength, ok := packetHeaderSize(dataType, simLen)
	if !ok {
		return nil, fmt.Errorf("unsupported jt1078 data type: %d", dataType)
	}
	if len(data) < headerLength {
		return nil, fmt.Errorf("jt1078 packet too short: need %d header bytes, got %d", headerLength, len(data))
	}

	var (
		timestamp uint64
		iFrameGap uint16
		frameGap  uint16
	)
	if dataType != DataTypePassthrough {
		timestamp = binary.BigEndian.Uint64(data[flagOffset+1 : flagOffset+9])
	}
	if dataType == DataTypeIFrame || dataType == DataTypePFrame || dataType == DataTypeBFrame {
		iFrameGap = binary.BigEndian.Uint16(data[flagOffset+9 : flagOffset+11])
		frameGap = binary.BigEndian.Uint16(data[flagOffset+11 : flagOffset+13])
	}

	bodyLenOffset := headerLength - 2
	bodyLength := int(binary.BigEndian.Uint16(data[bodyLenOffset : bodyLenOffset+2]))
	if headerLength+bodyLength > len(data) {
		return nil, fmt.Errorf("jt1078 body length %d exceeds packet length %d", bodyLength, len(data))
	}

	phone, err := decodeSIM(data[8:channelOffset])
	if err != nil {
		return nil, err
	}

	return &Packet{
		Seq:                binary.BigEndian.Uint16(data[6:8]),
		PayloadType:        data[5] & 0x7f,
		Phone:              phone,
		LogicChannel:       data[channelOffset],
		DataType:           dataType,
		SubpackageType:     subpackageType,
		Timestamp:          timestamp,
		LastIFrameInterval: iFrameGap,
		LastFrameInterval:  frameGap,
		Body:               data[headerLength : headerLength+bodyLength],
		consumed:           headerLength + bodyLength,
	}, nil
}

// packetHeaderSize 返回指定数据类型与 SIM 宽度下的固定头长度。
func packetHeaderSize(dataType uint8, simLen int) (int, bool) {
	base := 4 + 2 + 2 + simLen + 1 + 1 // magic + V/PT + SN + SIM + 通道 + 数据类型/分包
	switch dataType {
	case DataTypeIFrame, DataTypePFrame, DataTypeBFrame:
		return base + 8 + 2 + 2 + 2, true // 时间戳 + I 帧间隔 + 帧间隔 + 包体长度
	case DataTypeAudio:
		return base + 8 + 2, true // 时间戳 + 包体长度
	case DataTypePassthrough:
		return base + 2, true // 包体长度
	default:
		return 0, false
	}
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

// Push 接收一个分片包。
//
// 返回语义：
//   - 完整帧：返回帧载荷与 nil error；
//   - 尚未收齐：返回 nil 与 nil error（正常中间状态）；
//   - 缺片/超限/未知分包类型：返回 nil 与哨兵错误（ErrTruncatedPayload /
//     ErrFrameTooLarge / ErrCorruptPayload），调用方应丢弃当前帧。
func (a *frameAssembler) Push(pkt *Packet) ([]byte, error) {
	body := pkt.Body
	switch pkt.SubpackageType {
	case SubpackageAtomic:
		if len(body) > a.maxBytes {
			return nil, fmt.Errorf("atomic frame size %d exceeds limit %d: %w",
				len(body), a.maxBytes, ErrFrameTooLarge)
		}
		return body, nil
	case SubpackageFirst:
		var err error
		if _, ok := a.buffers[pkt.DataType]; ok {
			err = fmt.Errorf("previous frame missing last fragment: %w", ErrTruncatedPayload)
		}
		if len(body) > a.maxBytes {
			delete(a.buffers, pkt.DataType)
			return nil, fmt.Errorf("frame size %d exceeds limit %d: %w",
				len(body), a.maxBytes, ErrFrameTooLarge)
		}
		a.buffers[pkt.DataType] = append([]byte(nil), body...)
		return nil, err
	case SubpackageMiddle:
		buf, ok := a.buffers[pkt.DataType]
		if !ok {
			return nil, fmt.Errorf("middle fragment without first fragment: %w", ErrTruncatedPayload)
		}
		if len(buf)+len(body) > a.maxBytes {
			delete(a.buffers, pkt.DataType)
			return nil, fmt.Errorf("assembled frame size %d exceeds limit %d: %w",
				len(buf)+len(body), a.maxBytes, ErrFrameTooLarge)
		}
		a.buffers[pkt.DataType] = append(buf, body...)
		return nil, nil
	case SubpackageLast:
		buf, ok := a.buffers[pkt.DataType]
		if !ok {
			return nil, fmt.Errorf("last fragment without first fragment: %w", ErrTruncatedPayload)
		}
		delete(a.buffers, pkt.DataType)
		if len(buf)+len(body) > a.maxBytes {
			return nil, fmt.Errorf("assembled frame size %d exceeds limit %d: %w",
				len(buf)+len(body), a.maxBytes, ErrFrameTooLarge)
		}
		return append(buf, body...), nil
	default:
		return nil, fmt.Errorf("unknown subpackage type %d: %w", pkt.SubpackageType, ErrCorruptPayload)
	}
}
