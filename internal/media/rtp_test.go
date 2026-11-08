package media

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildTestPacket 构造一个 1078 RTP 包，头部按数据类型补齐。
func buildTestPacket(dataType, subpackage, payloadType uint8, seq uint16,
	phone string, channel uint8, body []byte) []byte {
	packet := make([]byte, 0, 30+len(body))
	packet = append(packet, jt1078Magic[:]...)
	packet = append(packet, 0x41)                       // V/P/X/CC，实际抓包为 0x41
	packet = append(packet, payloadType)                // M/PT
	packet = binary.BigEndian.AppendUint16(packet, seq) // SN
	// SIM BCD，12 位数字。
	for i := 0; i < 12; i += 2 {
		hi := phone[i] - '0'
		lo := phone[i+1] - '0'
		packet = append(packet, hi<<4|lo)
	}
	packet = append(packet, channel)
	packet = append(packet, dataType<<4|subpackage)

	switch dataType {
	case DataTypeIFrame, DataTypePFrame, DataTypeBFrame:
		packet = binary.BigEndian.AppendUint64(packet, 1000) // timestamp
		packet = binary.BigEndian.AppendUint16(packet, 500)
		packet = binary.BigEndian.AppendUint16(packet, 100)
		packet = binary.BigEndian.AppendUint16(packet, uint16(len(body)))
	case DataTypeAudio:
		packet = binary.BigEndian.AppendUint64(packet, 1000)
		packet = binary.BigEndian.AppendUint16(packet, uint16(len(body)))
	case DataTypePassthrough:
		packet = binary.BigEndian.AppendUint16(packet, uint16(len(body)))
	}
	return append(packet, body...)
}

func TestParsePacketHeaderLengths(t *testing.T) {
	body := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0xAA}

	video := buildTestPacket(DataTypeIFrame, SubpackageAtomic, PayloadTypeH264, 7, "188765430001", 1, body)
	pkt, err := ParsePacket(video)
	require.NoError(t, err)
	require.Equal(t, "188765430001", pkt.Phone)
	require.Equal(t, uint8(1), pkt.LogicChannel)
	require.Equal(t, DataTypeIFrame, pkt.DataType)
	require.Equal(t, SubpackageAtomic, pkt.SubpackageType)
	require.Equal(t, uint8(PayloadTypeH264), pkt.PayloadType)
	require.Equal(t, uint16(7), pkt.Seq)
	require.Equal(t, uint64(1000), pkt.Timestamp)
	require.Equal(t, body, pkt.Body)

	audio := buildTestPacket(DataTypeAudio, SubpackageAtomic, 6, 8, "188765430001", 1, body)
	pkt, err = ParsePacket(audio)
	require.NoError(t, err)
	require.Equal(t, DataTypeAudio, pkt.DataType)
	require.Equal(t, body, pkt.Body)

	penetrate := buildTestPacket(DataTypePassthrough, SubpackageAtomic, 0, 9, "188765430001", 1, body)
	pkt, err = ParsePacket(penetrate)
	require.NoError(t, err)
	require.Equal(t, DataTypePassthrough, pkt.DataType)
	require.Equal(t, body, pkt.Body)
}

func TestParsePacketRejectsInvalidInput(t *testing.T) {
	valid := buildTestPacket(DataTypeIFrame, SubpackageAtomic, PayloadTypeH264, 1, "188765430001", 1, []byte{1, 2, 3})

	cases := map[string][]byte{
		"too short":   valid[:17],
		"bad magic":   {0x00, 0x00, 0x01, 0xBA},
		"bad sim":     append(append([]byte(nil), valid[:8]...), 0xAB),
		"body length": append(append([]byte(nil), valid[:28]...), 0xFF, 0xFF),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if name == "bad sim" {
				raw = append(raw, valid[9:]...)
				raw[8] = 0xAB
			}
			if name == "bad magic" {
				raw = append(raw, valid[4:]...)
			}
			_, err := ParsePacket(raw)
			require.Error(t, err)
		})
	}
}

func TestParsePacketRTPVersionBits(t *testing.T) {
	base := buildTestPacket(DataTypeIFrame, SubpackageAtomic, PayloadTypeH264, 1, "188765430001", 1, []byte{1, 2, 3})

	cases := []struct {
		name    string
		version byte
		wantErr bool
	}{
		{"real capture v1 0x41", 0x41, false},
		{"standard rtp v2 0x81", 0x81, false},
		{"unsupported v0", 0x01, true},
		{"unsupported v3", 0xC1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := append([]byte(nil), base...)
			raw[4] = tc.version
			pkt, err := ParsePacket(raw)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, uint8(PayloadTypeH264), pkt.PayloadType)
			require.Equal(t, "188765430001", pkt.Phone)
		})
	}
}

func TestParsePacketRejectsEachMagicByte(t *testing.T) {
	valid := buildTestPacket(DataTypeIFrame, SubpackageAtomic, PayloadTypeH264, 1, "188765430001", 1, []byte{1, 2, 3})
	for i := 0; i < len(jt1078Magic); i++ {
		t.Run(fmt.Sprintf("magic_byte_%d", i), func(t *testing.T) {
			raw := append([]byte(nil), valid...)
			raw[i] ^= 0x01
			_, err := ParsePacket(raw)
			require.ErrorContains(t, err, "not a jt1078 packet")
		})
	}
}

// 三类头部的精确长度边界：刚好等于头长时 body 为空可解析，少 1 字节报错。
func TestParsePacketHeaderLengthBoundaries(t *testing.T) {
	const phone = "188765430001"
	cases := []struct {
		name     string
		dataType uint8
		payload  uint8
		wantLen  int
	}{
		{"video 30", DataTypeIFrame, PayloadTypeH264, 30},
		{"audio 26", DataTypeAudio, 6, 26},
		{"passthrough 18", DataTypePassthrough, 0, 18},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			emptyBody := buildTestPacket(tc.dataType, SubpackageAtomic, tc.payload, 1, phone, 1, nil)
			require.Len(t, emptyBody, tc.wantLen)
			pkt, err := ParsePacket(emptyBody)
			require.NoError(t, err)
			require.Empty(t, pkt.Body)
			_, err = ParsePacket(emptyBody[:tc.wantLen-1])
			require.Error(t, err)

			withBody := buildTestPacket(tc.dataType, SubpackageAtomic, tc.payload, 1, phone, 1, []byte{1, 2, 3})
			_, err = ParsePacket(withBody[:len(withBody)-1])
			require.Error(t, err, "declared body length exceeding packet must be rejected")
		})
	}
}

func TestParsePacketSIMBoundaries(t *testing.T) {
	valid := buildTestPacket(DataTypeIFrame, SubpackageAtomic, PayloadTypeH264, 1, "000000000000", 1, []byte{1, 2, 3})
	pkt, err := ParsePacket(valid)
	require.NoError(t, err)
	require.Equal(t, "000000000000", pkt.Phone)

	maxPhone := buildTestPacket(DataTypeIFrame, SubpackageAtomic, PayloadTypeH264, 1, "999999999999", 1, []byte{1, 2, 3})
	pkt, err = ParsePacket(maxPhone)
	require.NoError(t, err)
	require.Equal(t, "999999999999", pkt.Phone)

	// BCD 中出现 A~F 的 nibble 必须报错，不能解码成非数字手机号。
	invalidNibbles := []struct {
		name string
		pos  int
		val  byte
	}{
		{"high nibble", 8, 0xA1},
		{"low nibble", 8, 0x1A},
		{"last byte", 13, 0x9F},
	}
	for _, tc := range invalidNibbles {
		t.Run(tc.name, func(t *testing.T) {
			raw := append([]byte(nil), valid...)
			raw[tc.pos] = tc.val
			_, err := ParsePacket(raw)
			require.Error(t, err)
		})
	}
}

func TestFrameAssemblerReassemblesSubpackages(t *testing.T) {
	assembler := newFrameAssembler(1024)
	require.Nil(t, assembler.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageFirst, Body: []byte("aa")}))
	require.Nil(t, assembler.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageMiddle, Body: []byte("bb")}))
	require.Equal(t, []byte("aabbcc"), assembler.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageLast, Body: []byte("cc")}))

	// 原子帧直接返回。
	require.Equal(t, []byte("x"), assembler.Push(&Packet{DataType: DataTypePFrame, SubpackageType: SubpackageAtomic, Body: []byte("x")}))

	// 缺少 first 的 last 包被丢弃。
	require.Nil(t, assembler.Push(&Packet{DataType: DataTypePFrame, SubpackageType: SubpackageLast, Body: []byte("zz")}))

	// 超过上限时清空缓存，避免异常终端撑爆内存。
	small := newFrameAssembler(4)
	require.Nil(t, small.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageFirst, Body: []byte("1234")}))
	require.Nil(t, small.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageLast, Body: []byte("5")}))
}

// 分包乱序或缺片时不得拼出错误帧：middle/last 先到都丢弃，随后到达的 first 仍需等待。
func TestFrameAssemblerDropsOutOfOrderFragments(t *testing.T) {
	assembler := newFrameAssembler(1024)
	// 乱序：中间片和尾片先到，都因缺少首片被丢弃。
	require.Nil(t, assembler.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageMiddle, Body: []byte("missing-first")}))
	require.Nil(t, assembler.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageLast, Body: []byte("missing-first")}))
	// 新帧的 first 仍然正常缓存，last 缺 middle 也能收尾。
	require.Nil(t, assembler.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageFirst, Body: []byte("aa")}))
	require.Equal(t, []byte("aacc"), assembler.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageLast, Body: []byte("cc")}))

	// 缺片：只有 first 没有 last 时不产出帧；原子帧不消费 I 帧缓存。
	require.Nil(t, assembler.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageFirst, Body: []byte("orphan")}))
	require.Equal(t, []byte("p"), assembler.Push(&Packet{DataType: DataTypePFrame, SubpackageType: SubpackageAtomic, Body: []byte("p")}))
	// 下一个 I 帧尾片会消费残留的首片，消费后缓存清空。
	require.Equal(t, []byte("orphanzz"), assembler.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageLast, Body: []byte("zz")}))
	require.Nil(t, assembler.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageLast, Body: []byte("zz")}))

	// 重复的 first 覆盖旧缓存，不拼接两段首片。
	assembler = newFrameAssembler(1024)
	require.Nil(t, assembler.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageFirst, Body: []byte("aa")}))
	require.Nil(t, assembler.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageFirst, Body: []byte("bb")}))
	require.Equal(t, []byte("bbcc"), assembler.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageLast, Body: []byte("cc")}))
}

// 不同数据类型的分片缓存互相独立，且原子帧不破坏正在拼装的缓存。
func TestFrameAssemblerKeepsDataTypeBuffersSeparate(t *testing.T) {
	assembler := newFrameAssembler(1024)
	require.Nil(t, assembler.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageFirst, Body: []byte("ii")}))
	require.Nil(t, assembler.Push(&Packet{DataType: DataTypePFrame, SubpackageType: SubpackageFirst, Body: []byte("pp")}))
	require.Equal(t, []byte("x"), assembler.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageAtomic, Body: []byte("x")}))
	require.Equal(t, []byte("iicc"), assembler.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageLast, Body: []byte("cc")}))
	require.Equal(t, []byte("ppdd"), assembler.Push(&Packet{DataType: DataTypePFrame, SubpackageType: SubpackageLast, Body: []byte("dd")}))
}

// 超过 8MB 的单帧重组上限时必须丢弃并清空缓存，且超限后可重新开始正常收帧。
func TestFrameAssemblerEnforcesEightMegabyteLimit(t *testing.T) {
	first := make([]byte, maxFrameBytes-1)

	exact := newFrameAssembler(maxFrameBytes)
	require.Nil(t, exact.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageFirst, Body: first}))
	frame := exact.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageLast, Body: []byte{0x01}})
	require.Len(t, frame, maxFrameBytes)

	over := newFrameAssembler(maxFrameBytes)
	require.Nil(t, over.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageFirst, Body: make([]byte, maxFrameBytes)}))
	require.Nil(t, over.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageMiddle, Body: []byte{0x02}}))
	require.Empty(t, over.buffers)

	require.Nil(t, over.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageFirst, Body: []byte{1}}))
	require.Equal(t, []byte{1, 2}, over.Push(&Packet{DataType: DataTypeIFrame, SubpackageType: SubpackageLast, Body: []byte{2}}))
}
