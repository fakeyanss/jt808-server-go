package media

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// MPEG-PS / PES 容器起始码。
const (
	psPackStartCode    = 0xBA
	psSystemHeaderCode = 0xBB
	psProgramStreamMap = 0xBC
	psPaddingCode      = 0xBE
	psProgramEndCode   = 0xB9
	pesVideoStreamMin  = 0xE0
	pesVideoStreamMax  = 0xEF
	pesAudioStreamMin  = 0xC0
	pesAudioStreamMax  = 0xDF
)

// ExtractH264 从一帧完整的 1078 视频载荷中还原 H.264 裸流（Annex B）。
//
// 支持两种实际载荷形态：
//  1. 标准 MPEG-PS 封装：剥离 pack/system header 与 PES 头，拼接 0xE0~0xEF 视频 PES 的 ES；
//  2. 终端直接发送 Annex B H.264 ES：原样返回。
//
// 仅支持 H.264；H.265 与其他载荷形态返回 ErrUnsupportedPayload。
func ExtractH264(payload []byte) ([]byte, error) {
	if !hasPSPack(payload) {
		if hasAnnexBStartCode(payload) {
			return payload, nil
		}
		return nil, fmt.Errorf("unsupported 1078 video payload: neither PS nor Annex B")
	}

	var out []byte
	idx := findPSStart(payload, 0)
	for idx >= 0 && idx+4 <= len(payload) {
		code := payload[idx+3]
		switch {
		case code == psPackStartCode:
			next, ok := skipPackHeader(payload, idx)
			if !ok {
				return out, nil
			}
			idx = next
		case code == psSystemHeaderCode || code == psProgramStreamMap || code == psPaddingCode:
			next, ok := skipLengthPrefixedHeader(payload, idx)
			if !ok {
				return out, nil
			}
			idx = next
		case code == psProgramEndCode:
			idx += 4
		case code >= pesVideoStreamMin && code <= pesVideoStreamMax:
			es, next, ok := readVideoPES(payload, idx)
			idx = next
			if ok {
				out = append(out, es...)
			}
		case code >= pesAudioStreamMin && code <= pesAudioStreamMax:
			next, ok := skipLengthPrefixedHeader(payload, idx)
			if !ok {
				return out, nil
			}
			idx = next
		default:
			next := findPSStart(payload, idx+1)
			if next < 0 {
				return out, nil
			}
			idx = next
		}
		if idx >= len(payload) {
			break
		}
		next := findPSStart(payload, idx)
		if next < 0 {
			break
		}
		idx = next
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no video elementary stream found in PS payload")
	}
	return out, nil
}

func hasPSPack(payload []byte) bool {
	return bytes.Contains(payload, []byte{0x00, 0x00, 0x01, psPackStartCode})
}

func hasAnnexBStartCode(payload []byte) bool {
	return bytes.HasPrefix(payload, []byte{0x00, 0x00, 0x00, 0x01}) ||
		bytes.HasPrefix(payload, []byte{0x00, 0x00, 0x01})
}

// findPSStart 从 from 开始查找下一个容器起始码（0x00 0x00 0x01 + 容器码）。
// 只匹配 >= 0xB0 的容器码，避免把 H.264 NAL 起始码误判为容器边界。
func findPSStart(payload []byte, from int) int {
	for i := from; i+4 <= len(payload); i++ {
		if payload[i] != 0x00 || payload[i+1] != 0x00 || payload[i+2] != 0x01 {
			continue
		}
		if payload[i+3] >= 0xB0 {
			return i
		}
	}
	return -1
}

// skipPackHeader 跳过 MPEG-2 pack header（4 字节起始码 + 10 字节固定头 + stuffing）。
func skipPackHeader(payload []byte, idx int) (int, bool) {
	if idx+14 > len(payload) {
		return 0, false
	}
	stuffing := int(payload[idx+13] & 0x07)
	next := idx + 14 + stuffing
	if next > len(payload) {
		return 0, false
	}
	return next, true
}

// skipLengthPrefixedHeader 跳过 system header / PSM / padding / audio PES。
func skipLengthPrefixedHeader(payload []byte, idx int) (int, bool) {
	if idx+6 > len(payload) {
		return 0, false
	}
	length := int(binary.BigEndian.Uint16(payload[idx+4 : idx+6]))
	next := idx + 6 + length
	if next > len(payload) {
		return 0, false
	}
	return next, true
}

// readVideoPES 读取视频 PES 的 ES 数据。
// PES_packet_length 为 0 时表示包长不定，需向后寻找下一个容器起始码。
func readVideoPES(payload []byte, idx int) ([]byte, int, bool) {
	if idx+9 > len(payload) {
		return nil, len(payload), false
	}
	pesLength := int(binary.BigEndian.Uint16(payload[idx+4 : idx+6]))
	headerLength := int(payload[idx+8])
	dataStart := idx + 9 + headerLength
	if dataStart > len(payload) {
		return nil, len(payload), false
	}

	dataEnd := len(payload)
	if pesLength > 0 {
		dataEnd = idx + 6 + pesLength
		if dataEnd > len(payload) {
			dataEnd = len(payload)
		}
	} else if next := findPSStart(payload, dataStart); next >= 0 {
		dataEnd = next
	}
	if dataEnd < dataStart {
		dataEnd = dataStart
	}

	es := trimToAnnexB(payload[dataStart:dataEnd])
	return es, dataEnd, len(es) > 0
}

// trimToAnnexB 保证输出从 H.264 起始码开始；PES 头部残留前缀会被裁掉。
func trimToAnnexB(es []byte) []byte {
	if len(es) == 0 {
		return nil
	}
	if bytes.HasPrefix(es, []byte{0x00, 0x00, 0x00, 0x01}) ||
		bytes.HasPrefix(es, []byte{0x00, 0x00, 0x01}) {
		return es
	}
	if idx := bytes.Index(es, []byte{0x00, 0x00, 0x00, 0x01}); idx >= 0 && idx <= 64 {
		return es[idx:]
	}
	if idx := bytes.Index(es, []byte{0x00, 0x00, 0x01}); idx >= 0 && idx <= 64 {
		return es[idx:]
	}
	return nil
}
