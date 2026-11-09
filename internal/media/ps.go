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
// 语义区分：
//   - 正常结束（如 PS program end code）不返回错误，可能返回空数据；
//   - 截断/损坏返回 ErrTruncatedPayload 或 ErrCorruptPayload，且不会返回部分 ES；
//   - 非 PS/非 Annex B 返回 ErrUnsupportedPayload。
func ExtractH264(payload []byte) ([]byte, error) {
	if !hasPSPack(payload) {
		if hasAnnexBStartCode(payload) {
			return payload, nil
		}
		return nil, fmt.Errorf("payload is neither MPEG-PS nor Annex B: %w", ErrUnsupportedPayload)
	}

	var out []byte
	ended := false
	idx := findPSStart(payload, 0)
	for idx >= 0 && idx+4 <= len(payload) {
		code := payload[idx+3]
		switch {
		case code == psPackStartCode:
			next, err := skipPackHeader(payload, idx)
			if err != nil {
				return nil, err
			}
			idx = next
		case code == psSystemHeaderCode || code == psProgramStreamMap || code == psPaddingCode:
			next, err := skipLengthPrefixedHeader(payload, idx)
			if err != nil {
				return nil, err
			}
			idx = next
		case code == psProgramEndCode:
			// PS 流正常结束，不是错误。
			ended = true
		case code >= pesVideoStreamMin && code <= pesVideoStreamMax:
			es, next, err := readVideoPES(payload, idx)
			if err != nil {
				return nil, err
			}
			idx = next
			out = append(out, es...)
		case code >= pesAudioStreamMin && code <= pesAudioStreamMax:
			// 视频提取路径不需要音频 ES，按 PES_packet_length 整段跳过。
			next, err := skipLengthPrefixedHeader(payload, idx)
			if err != nil {
				return nil, err
			}
			idx = next
		default:
			next := findPSStart(payload, idx+1)
			if next < 0 {
				ended = true
				break
			}
			idx = next
		}
		if ended || idx >= len(payload) {
			break
		}
		next := findPSStart(payload, idx)
		if next < 0 {
			break
		}
		idx = next
	}

	if len(out) == 0 {
		if ended {
			// 正常结束且本帧没有可写 ES（例如只有 program end code）。
			return nil, nil
		}
		return nil, fmt.Errorf("no video elementary stream found in PS payload: %w", ErrCorruptPayload)
	}
	return out, nil
}

// ExtractAudio 从一帧完整的 1078 音频载荷中提取音频 ES。
//
// 支持两种实际载荷形态：
//  1. 标准 MPEG-PS 封装：取 0xC0~0xDF 音频 PES 的 ES；
//  2. 终端直接发送裸音频载荷（G.711A/U 或带 ADTS 的 AAC）：原样返回。
//
// 本函数只负责解封装，不判断编码格式；编码格式由 1078 载荷类型决定。
func ExtractAudio(payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	if findPSStart(payload, 0) != 0 {
		// 裸音频载荷：G.711 是纯采样数据，不能靠“是否包含 pack 起始码”判断，
		// 否则随机采样里恰好出现的 00 00 01 BA 会被误判成 PS 容器。
		return payload, nil
	}
	return extractPSAudio(payload)
}

// extractPSAudio 遍历 PS 容器并拼接所有音频 PES 的 ES。
func extractPSAudio(payload []byte) ([]byte, error) {
	var out []byte
	ended := false
	idx := findPSStart(payload, 0)
	for idx >= 0 && idx+4 <= len(payload) {
		code := payload[idx+3]
		switch {
		case code == psPackStartCode:
			next, err := skipPackHeader(payload, idx)
			if err != nil {
				return nil, err
			}
			idx = next
		case code == psSystemHeaderCode || code == psProgramStreamMap || code == psPaddingCode:
			next, err := skipLengthPrefixedHeader(payload, idx)
			if err != nil {
				return nil, err
			}
			idx = next
		case code == psProgramEndCode:
			ended = true
		case code >= pesAudioStreamMin && code <= pesAudioStreamMax:
			es, next, err := readPES(payload, idx, false)
			if err != nil {
				return nil, err
			}
			idx = next
			out = append(out, es...)
		case code >= pesVideoStreamMin && code <= pesVideoStreamMax:
			// 音频提取路径跳过视频 PES，避免把视频数据混入音频文件。
			_, next, err := readPES(payload, idx, false)
			if err != nil {
				return nil, err
			}
			idx = next
		default:
			next := findPSStart(payload, idx+1)
			if next < 0 {
				ended = true
				break
			}
			idx = next
		}
		if ended || idx >= len(payload) {
			break
		}
		next := findPSStart(payload, idx)
		if next < 0 {
			break
		}
		idx = next
	}
	if len(out) == 0 {
		if ended {
			return nil, nil
		}
		return nil, fmt.Errorf("no audio elementary stream found in PS payload: %w", ErrCorruptPayload)
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
// 头部或 stuffing 被截断时返回 ErrTruncatedPayload。
func skipPackHeader(payload []byte, idx int) (int, error) {
	if idx+14 > len(payload) {
		return 0, fmt.Errorf("pack header at offset %d truncated: need 14 bytes, got %d: %w",
			idx, len(payload)-idx, ErrTruncatedPayload)
	}
	stuffing := int(payload[idx+13] & 0x07)
	next := idx + 14 + stuffing
	if next > len(payload) {
		return 0, fmt.Errorf("pack stuffing at offset %d truncated: need %d bytes, got %d: %w",
			idx, next, len(payload), ErrTruncatedPayload)
	}
	return next, nil
}

// skipLengthPrefixedHeader 跳过 system header / PSM / padding / audio PES。
// 头部或声明长度超出剩余字节时返回 ErrTruncatedPayload。
func skipLengthPrefixedHeader(payload []byte, idx int) (int, error) {
	if idx+6 > len(payload) {
		return 0, fmt.Errorf("PS header 0x%02X at offset %d truncated: need 6 bytes, got %d: %w",
			payload[idx+3], idx, len(payload)-idx, ErrTruncatedPayload)
	}
	length := int(binary.BigEndian.Uint16(payload[idx+4 : idx+6]))
	next := idx + 6 + length
	if next > len(payload) {
		return 0, fmt.Errorf("PS header 0x%02X at offset %d declares %d bytes but only %d remain: %w",
			payload[idx+3], idx, length, len(payload)-idx-6, ErrTruncatedPayload)
	}
	return next, nil
}

// readVideoPES 读取视频 PES 的 ES 数据，并要求 ES 以 Annex B 起始码开头。
func readVideoPES(payload []byte, idx int) ([]byte, int, error) {
	return readPES(payload, idx, true)
}

// readPES 读取 PES 的 ES 数据。
// PES_packet_length 为 0 时表示包长不定，需向后寻找下一个容器起始码。
// requireAnnexB 为 true 时用于视频，会裁掉头部残留前缀并把无起始码视为损坏。
func readPES(payload []byte, idx int, requireAnnexB bool) ([]byte, int, error) {
	if idx+9 > len(payload) {
		return nil, len(payload), fmt.Errorf("PES at offset %d truncated: need 9 header bytes, got %d: %w",
			idx, len(payload)-idx, ErrTruncatedPayload)
	}
	pesLength := int(binary.BigEndian.Uint16(payload[idx+4 : idx+6]))
	headerLength := int(payload[idx+8])
	dataStart := idx + 9 + headerLength
	if dataStart > len(payload) {
		return nil, len(payload), fmt.Errorf("PES at offset %d header length %d exceeds remaining payload: %w",
			idx, headerLength, ErrTruncatedPayload)
	}

	dataEnd := len(payload)
	if pesLength > 0 {
		dataEnd = idx + 6 + pesLength
		if dataEnd > len(payload) {
			return nil, len(payload), fmt.Errorf("PES at offset %d declares %d bytes but only %d remain: %w",
				idx, pesLength, len(payload)-idx-6, ErrTruncatedPayload)
		}
	} else if next := findPSStart(payload, dataStart); next >= 0 {
		dataEnd = next
	}
	if dataEnd < dataStart {
		return nil, dataStart, fmt.Errorf("PES at offset %d declares %d bytes, shorter than its header: %w",
			idx, pesLength, ErrCorruptPayload)
	}

	raw := payload[dataStart:dataEnd]
	if !requireAnnexB {
		return raw, dataEnd, nil
	}
	es := trimToAnnexB(raw)
	if len(raw) > 0 && len(es) == 0 {
		return nil, dataEnd, fmt.Errorf("video PES at offset %d carries no Annex B start code: %w",
			idx, ErrCorruptPayload)
	}
	return es, dataEnd, nil
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
