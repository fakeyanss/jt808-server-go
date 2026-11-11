package playback

import (
	"encoding/binary"
	"errors"
)

// H.264 NAL 类型。
const (
	NALTypeIDR  = uint8(5)
	NALTypeSPS  = uint8(7)
	NALTypePPS  = uint8(8)
	nalTypeMask = uint8(0x1f)
)

// startCode 是补写参数集时统一使用的 4 字节 Annex B 起始码。
var startCode = []byte{0x00, 0x00, 0x00, 0x01}

// NAL 是一个 Annex B NAL 单元；Body 引用原始帧数据，不复制。
type NAL struct {
	Typ  uint8
	Body []byte
}

// SplitAnnexB 按 3/4 字节起始码切分 Annex B 码流。
// 数据不以起始码开头时返回 nil，调用方按原样处理。
func SplitAnnexB(data []byte) []NAL {
	start, scLen := nextStartCode(data, 0)
	if start < 0 {
		return nil
	}
	units := make([]NAL, 0, 8)
	for start >= 0 {
		bodyStart := start + scLen
		next, nextLen := nextStartCode(data, bodyStart)
		end := len(data)
		if next >= 0 {
			end = next
		}
		if end > bodyStart {
			units = append(units, NAL{Typ: data[bodyStart] & nalTypeMask, Body: data[bodyStart:end]})
		}
		if next < 0 {
			break
		}
		start, scLen = next, nextLen
	}
	return units
}

// ExtractParamSets 从 Annex B 帧里提取 SPS/PPS（保留起始码）。
func ExtractParamSets(frame []byte) ParamSets {
	var out ParamSets
	for _, nal := range SplitAnnexB(frame) {
		switch nal.Typ {
		case NALTypeSPS:
			out.SPS = WithStartCode(nal.Body)
		case NALTypePPS:
			out.PPS = WithStartCode(nal.Body)
		}
	}
	return out
}

// WithStartCode 把 NAL 体复制成带 4 字节起始码的独立切片。
func WithStartCode(body []byte) []byte {
	out := make([]byte, 0, len(startCode)+len(body))
	out = append(out, startCode...)
	return append(out, body...)
}

// NALBody 去掉 NAL 的 Annex B 起始码。
func NALBody(data []byte) []byte {
	start, length := nextStartCode(data, 0)
	if start < 0 {
		return data
	}
	return data[start+length:]
}

// IsKeyframe 判断 Annex B 帧内是否含 IDR。
func IsKeyframe(frame []byte) bool {
	for _, nal := range SplitAnnexB(frame) {
		if nal.Typ == NALTypeIDR {
			return true
		}
	}
	return false
}

// AVCDecoderConfigurationRecord 由 SPS/PPS 构造 FLV/RTMP 共用的 AVCC 配置记录。
//
// 字段：configurationVersion、profile/compat/level、lengthSizeMinusOne、
// numOfSequenceParameterSets、SPS、numOfPictureParameterSets、PPS。
func AVCDecoderConfigurationRecord(params ParamSets) ([]byte, error) {
	sps := NALBody(params.SPS)
	pps := NALBody(params.PPS)
	if len(sps) < 4 || len(pps) == 0 {
		return nil, errors.New("playback: incomplete SPS/PPS")
	}
	record := make([]byte, 0, 16+len(sps)+len(pps))
	record = append(record,
		0x01,   // configurationVersion
		sps[1], // AVCProfileIndication
		sps[2], // profile_compatibility
		sps[3], // AVCLevelIndication
		0xFF,   // reserved(6) + lengthSizeMinusOne=3
		0xE1,   // reserved(3) + numOfSequenceParameterSets=1
		byte(len(sps)>>8), byte(len(sps)),
	)
	record = append(record, sps...)
	record = append(record, 0x01, byte(len(pps)>>8), byte(len(pps)))
	record = append(record, pps...)
	return record, nil
}

// VideoTagBody 把 Annex B 帧转成 FLV/RTMP 共用的视频消息体（AVCC）。
//
// 帧内自带的 SPS/PPS 会被剔除：参数集只走解码配置记录，避免同一组参数集
// 在流里出现两种编码形态。若整帧只有参数集，返回 nil。
func VideoTagBody(annexB []byte, keyframe bool) ([]byte, error) {
	annex := SplitAnnexB(annexB)
	if len(annex) == 0 {
		// 裸 NAL（无起始码）按单 NAL 处理。
		if len(annexB) == 0 {
			return nil, nil
		}
		annex = []NAL{{Typ: annexB[0] & nalTypeMask, Body: annexB}}
	}
	body := make([]byte, 0, 5+len(annexB)+16)
	if keyframe {
		body = append(body, 0x17) // keyframe + AVC
	} else {
		body = append(body, 0x27) // inter frame + AVC
	}
	body = append(body, 0x01, 0x00, 0x00, 0x00) // AVC NALU
	written := 0
	for _, nal := range annex {
		if nal.Typ == NALTypeSPS || nal.Typ == NALTypePPS || nal.Typ == 9 || nal.Typ == 12 {
			continue
		}
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(nal.Body)))
		body = append(body, length[:]...)
		body = append(body, nal.Body...)
		written++
	}
	if written == 0 {
		return nil, nil
	}
	return body, nil
}

// AudioTagBody 构造 FLV/RTMP 共用的 AAC 音频消息体。
func AudioTagBody(aacRaw []byte) []byte {
	body := make([]byte, 0, 2+len(aacRaw))
	body = append(body, 0xAF, 0x01)
	return append(body, aacRaw...)
}

// AudioSequenceTagBody 构造 AAC AudioSpecificConfig 的解码配置消息体。
func AudioSequenceTagBody(config []byte) []byte {
	body := make([]byte, 0, 2+len(config))
	body = append(body, 0xAF, 0x00)
	return append(body, config...)
}

// ParseADTS 去掉 ADTS 头并生成 AudioSpecificConfig。
//
// AudioSpecificConfig（2 字节）：
// audioObjectType(5) + samplingFrequencyIndex(4) + channelConfiguration(4) + 补齐。
func ParseADTS(frame []byte) (raw, config []byte, ok bool) {
	if len(frame) < 7 || frame[0] != 0xFF || frame[1]&0xF0 != 0xF0 {
		return nil, nil, false
	}
	protectionAbsent := frame[1]&0x01 == 1
	profile := frame[2] >> 6
	freqIndex := (frame[2] >> 2) & 0x0F
	channelConfig := ((frame[2] & 0x01) << 2) | (frame[3] >> 6)
	frameLength := (int(frame[3]&0x03) << 11) | (int(frame[4]) << 3) | int(frame[5]>>5)
	headerLength := 7
	if !protectionAbsent {
		headerLength = 9
	}
	if frameLength < headerLength || frameLength > len(frame) {
		frameLength = len(frame)
	}
	audioObjectType := profile + 1
	config = []byte{
		byte(audioObjectType<<3) | byte(freqIndex>>1),
		byte(freqIndex<<7) | byte(channelConfig<<3),
	}
	return frame[headerLength:frameLength], config, true
}

// nextStartCode 从 from 开始查找下一个 Annex B 起始码。
// 返回起始位置与起始码长度；紧邻的前导 0 视为 4 字节起始码的一部分。
func nextStartCode(data []byte, from int) (pos, length int) {
	for i := from; i+3 <= len(data); i++ {
		if data[i] != 0x00 || data[i+1] != 0x00 || data[i+2] != 0x01 {
			continue
		}
		if i > 0 && data[i-1] == 0x00 {
			return i - 1, 4
		}
		return i, 3
	}
	return -1, 0
}
