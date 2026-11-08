package media

import (
	"encoding/binary"
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
