package protocol

import (
	"fmt"
)

type PacketCodec interface {
	Decode([]byte) (JT808Msg, error)
	Encode(JT808Msg) ([]byte, error)
}

type JT808PacketCodec struct {
}

func NewJT808PacketCodec() *JT808PacketCodec {
	return &JT808PacketCodec{}
}

// Decode JT808 packet.
//
// 反转义 -> 校验 -> 反序列化
func (pc *JT808PacketCodec) Decode(packet []byte) (JT808Msg, error) {
	pkt := pc.unescape(packet)

	pkt, err := pc.verify(pkt)
	if err != nil {
		return nil, err
	}

	m := &Msg0100{}
	err = m.Decode(pkt)
	if err != nil {
		return nil, err
	}

	return m, nil
}

// Encode JT808 packet.
//
// 序列化 -> 生成校验码 -> 转义
func (pc *JT808PacketCodec) Encode(cmd JT808Cmd) ([]byte, error) {
	pkt, err := cmd.Encode()
	if err != nil {
		return nil, err
	}

	pkt = pc.genVerifier(pkt)

	pkt = pc.escape(pkt)

	return pkt, nil
}

// Unescape JT808 packet.
//
// 去除前后标识符0x7e, 并将转义的数据包反转义:
//
//	0x7d0x02 -> 0x7e
//	0x7d0x01 -> 0x7d
func (pc *JT808PacketCodec) unescape(src []byte) []byte {
	dst := make([]byte, 0)
	i, n := 1, len(src)
	for i < n-1 {
		if i < n-2 && src[i] == 0x7d && src[i+1] == 0x02 {
			dst = append(dst, 0x7e)
			i += 2
		} else if i < n-2 && src[i] == 0x7d && src[i+1] == 0x01 {
			dst = append(dst, 0x7d)
			i += 2
		} else {
			dst = append(dst, src[i])
			i++
		}
	}
	return dst
}

// Escape JT808 packet.
//
// 转义数据包：
//
//	0x7e -> 0x7d0x02
//	0x7d -> 0x7d0x01
//
// 并加上前后标识符0x7e
func (pc *JT808PacketCodec) escape(src []byte) []byte {
	dst := make([]byte, 0)
	dst = append(dst, 0x7e)
	for _, v := range src {
		if v == 0x7e {
			dst = append(dst, 0x7d, 0x02)
		} else if v == 0x7d {
			dst = append(dst, 0x7d, 0x01)
		} else {
			dst = append(dst, v)
		}
	}
	dst = append(dst, 0x7e)
	return dst
}

// 消息体异或校验，并去掉校验码
func (pc *JT808PacketCodec) verify(pkt []byte) ([]byte, error) {
	n := len(pkt)
	if n == 0 {
		return nil, fmt.Errorf("EmptyPacketWhenVerify")
	}
	expected := pkt[n-1]
	var actual byte
	for _, v := range pkt[:n-1] {
		actual ^= v
	}
	if expected == actual {
		return pkt[:n-1], nil
	}
	return nil, fmt.Errorf("verify error, expect=%v, actual=%v", expected, actual)
}

func (pc *JT808PacketCodec) genVerifier(pkt []byte) []byte {
	var code byte
	for _, v := range pkt {
		code ^= v
	}
	pkt = append(pkt, code)
	return pkt
}
