package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg8105EncodeDecode(t *testing.T) {
	msg := &Msg8105{Header: genMsgHeader(0x8105), CommandWord: 2, CommandParam: "example.com;7611;TOKEN"}
	encoded, err := msg.Encode()
	require.NoError(t, err)
	decoded := &Msg8105{}
	require.NoError(t, decoded.Decode(&PacketData{Header: msg.Header, Body: encoded[len(encoded)-len([]byte(msg.CommandParam))-1:]}))
	require.Equal(t, msg.CommandWord, decoded.CommandWord)
	require.Equal(t, msg.CommandParam, decoded.CommandParam)
	require.Error(t, (&Msg8105{}).Decode(&PacketData{Body: nil}))
}

// 类型化参数按标准槽位编码：命令字 + 13 个以分号分隔的槽位 + 扩展槽位。
func TestMsg8105TypedParametersEncodeDecode(t *testing.T) {
	msg := &Msg8105{
		Header:      genMsgHeader(0x8105),
		CommandWord: CommandWordConnectServer,
		Parameters: []CommandParameter{
			{ID: CommandParamServerAddress, Length: uint8(len("example.com")), Value: []byte("example.com")},
			{ID: CommandParamTCPPort, Length: 2, Value: []byte{0x1D, 0xBB}},
			// 0x0D 及以上为标准未定义的厂商扩展，必须原样保留。
			{ID: 13, Length: 5, Value: []byte("ext-1")},
		},
	}
	encoded, err := msg.Encode()
	require.NoError(t, err)

	body := encoded[len(encoded)-int(msg.GetHeader().Attr.BodyLength):]
	expected := []byte{CommandWordConnectServer}
	expected = append(expected, []byte(";;;;example.com;")...)
	expected = append(expected, 0x1D, 0xBB, ';')
	expected = append(expected, []byte(";;;;;;;ext-1;")...)
	require.Equal(t, expected, body)

	decoded := &Msg8105{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.Parameters, decoded.Parameters)
}

// 命令参数长度必须与实际值一致，否则拒绝编码。
func TestMsg8105TypedParameterLengthMismatch(t *testing.T) {
	msg := &Msg8105{
		Header:      genMsgHeader(0x8105),
		CommandWord: CommandWordConnectServer,
		Parameters: []CommandParameter{
			{ID: CommandParamServerAddress, Length: 3, Value: []byte("example.com")},
		},
	}
	_, err := msg.Encode()
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not match value length")
}

// 同一槽位重复下发属于调用方错误。
func TestMsg8105DuplicateParameterID(t *testing.T) {
	msg := &Msg8105{
		Header:      genMsgHeader(0x8105),
		CommandWord: CommandWordConnectServer,
		Parameters: []CommandParameter{
			{ID: CommandParamServerAddress, Length: 1, Value: []byte("a")},
			{ID: CommandParamServerAddress, Length: 1, Value: []byte("b")},
		},
	}
	_, err := msg.Encode()
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate command parameter")
}

// 只有命令字 1/2 定义了命令参数，其它命令字不接受类型化参数。
func TestMsg8105TypedParameterUnsupportedCommandWord(t *testing.T) {
	msg := &Msg8105{
		Header:      genMsgHeader(0x8105),
		CommandWord: CommandWordReset,
		Parameters:  []CommandParameter{{ID: 0, Length: 1, Value: []byte{1}}},
	}
	_, err := msg.Encode()
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not support typed parameters")
}

// 解码把未知槽位（>= 13）原样保留，重新编码后字节一致。
func TestMsg8105UnknownParameterIDRoundTrip(t *testing.T) {
	body := []byte{CommandWordConnectServer}
	body = append(body, []byte(";;;;;;;;;;;;;xvendor;")...)

	decoded := &Msg8105{}
	require.NoError(t, decoded.Decode(&PacketData{Header: genMsgHeader(0x8105), Body: body}))
	require.Len(t, decoded.Parameters, 1)
	require.Equal(t, uint8(13), decoded.Parameters[0].ID)
	require.Equal(t, []byte("xvendor"), decoded.Parameters[0].Value)
	require.Equal(t, uint8(7), decoded.Parameters[0].Length)

	encoded, err := (&Msg8105{Header: genMsgHeader(0x8105), CommandWord: decoded.CommandWord, Parameters: decoded.Parameters}).Encode()
	require.NoError(t, err)
	expectedBody := append([]byte{CommandWordConnectServer}, []byte(";;;;;;;;;;;;;xvendor;")...)
	require.Equal(t, expectedBody, encoded[len(encoded)-len(expectedBody):])
}

// 末尾没有分号时最后一个参数仍应被解码。
func TestMsg8105DecodeWithoutTrailingSeparator(t *testing.T) {
	decoded := &Msg8105{}
	require.NoError(t, decoded.Decode(&PacketData{
		Header: genMsgHeader(0x8105),
		Body:   append([]byte{CommandWordConnectServer}, []byte("addr")...),
	}))
	require.Len(t, decoded.Parameters, 1)
	require.Equal(t, CommandParamConnectControl, decoded.Parameters[0].ID)
	require.Equal(t, []byte("addr"), decoded.Parameters[0].Value)
}

// 保留路径：只设置 CommandParam 时，编码结果与历史实现完全一致。
func TestMsg8105RawCommandParamCompatibility(t *testing.T) {
	msg := &Msg8105{Header: genMsgHeader(0x8105), CommandWord: 2, CommandParam: "example.com;7611;TOKEN"}
	encoded, err := msg.Encode()
	require.NoError(t, err)
	require.Equal(t, append([]byte{2}, []byte("example.com;7611;TOKEN")...),
		encoded[len(encoded)-int(msg.GetHeader().Attr.BodyLength):])
}

func TestCommandParameterName(t *testing.T) {
	require.Equal(t, "服务器地址", CommandParameterName(CommandParamServerAddress))
	require.Equal(t, "连接到指定服务器时限", CommandParameterName(CommandParamConnectTimeout))
	require.Empty(t, CommandParameterName(13))
}
