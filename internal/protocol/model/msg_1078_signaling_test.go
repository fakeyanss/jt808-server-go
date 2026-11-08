package model

import (
	stdhex "encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/stretchr/testify/require"
)

func TestMsg9101RoundTrip(t *testing.T) {
	msg := &Msg9101{
		Header:         genMsgHeader(0x9101),
		ServerIP:       "192.168.1.100",
		TCPPort:        9101,
		UDPPort:        9102,
		LogicChannelID: 1,
		DataType:       0,
		StreamType:     1,
	}
	decoded := &Msg9101{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.ServerIP, decoded.ServerIP)
	require.Equal(t, uint8(len(msg.ServerIP)), decoded.ServerIPLength)
	require.Equal(t, msg.TCPPort, decoded.TCPPort)
	require.Equal(t, msg.UDPPort, decoded.UDPPort)
	require.Equal(t, msg.LogicChannelID, decoded.LogicChannelID)
	require.Equal(t, msg.DataType, decoded.DataType)
	require.Equal(t, msg.StreamType, decoded.StreamType)
}

func TestMsg9102RoundTrip(t *testing.T) {
	msg := &Msg9102{
		Header:         genMsgHeader(0x9102),
		LogicChannelID: 2,
		ControlCmd:     1,
		CloseMediaType: 2,
		StreamType:     0,
	}
	decoded := &Msg9102{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.LogicChannelID, decoded.LogicChannelID)
	require.Equal(t, msg.ControlCmd, decoded.ControlCmd)
	require.Equal(t, msg.CloseMediaType, decoded.CloseMediaType)
	require.Equal(t, msg.StreamType, decoded.StreamType)
}

func TestMsg9105RoundTrip(t *testing.T) {
	msg := &Msg9105{
		Header:         genMsgHeader(0x9105),
		LogicChannelID: 1,
		PacketLossRate: 3,
	}
	decoded := &Msg9105{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.LogicChannelID, decoded.LogicChannelID)
	require.Equal(t, msg.PacketLossRate, decoded.PacketLossRate)
}

func TestMsg9201RoundTrip(t *testing.T) {
	start := time.Date(2026, 11, 8, 10, 0, 0, 0, time.Local)
	end := time.Date(2026, 11, 8, 11, 0, 0, 0, time.Local)
	msg := &Msg9201{
		Header:         genMsgHeader(0x9201),
		ServerIP:       "10.0.0.1",
		TCPPort:        7200,
		UDPPort:        7201,
		LogicChannelID: 1,
		MediaType:      2,
		StreamType:     1,
		StorageType:    1,
		PlaybackMode:   0,
		PlaybackSpeed:  1,
		StartTime:      &start,
		EndTime:        &end,
	}
	decoded := &Msg9201{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.ServerIP, decoded.ServerIP)
	require.Equal(t, msg.TCPPort, decoded.TCPPort)
	require.Equal(t, msg.UDPPort, decoded.UDPPort)
	require.Equal(t, msg.LogicChannelID, decoded.LogicChannelID)
	require.Equal(t, msg.MediaType, decoded.MediaType)
	require.Equal(t, msg.StreamType, decoded.StreamType)
	require.Equal(t, msg.StorageType, decoded.StorageType)
	require.Equal(t, msg.PlaybackMode, decoded.PlaybackMode)
	require.Equal(t, msg.PlaybackSpeed, decoded.PlaybackSpeed)
	require.Equal(t, hex.FormatTime(start), hex.FormatTime(*decoded.StartTime))
	require.Equal(t, hex.FormatTime(end), hex.FormatTime(*decoded.EndTime))
}

func TestMsg9202RoundTrip(t *testing.T) {
	drag := time.Date(2026, 11, 8, 10, 30, 0, 0, time.Local)
	msg := &Msg9202{
		Header:         genMsgHeader(0x9202),
		LogicChannelID: 3,
		PlayControl:    5,
		PlaySpeed:      0,
		DragPosition:   &drag,
	}
	decoded := &Msg9202{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.LogicChannelID, decoded.LogicChannelID)
	require.Equal(t, msg.PlayControl, decoded.PlayControl)
	require.Equal(t, msg.PlaySpeed, decoded.PlaySpeed)
	require.Equal(t, hex.FormatTime(drag), hex.FormatTime(*decoded.DragPosition))
}

func TestMsg9206RoundTrip(t *testing.T) {
	start := time.Date(2026, 11, 8, 9, 0, 0, 0, time.Local)
	end := time.Date(2026, 11, 8, 10, 0, 0, 0, time.Local)
	msg := &Msg9206{
		Header:         genMsgHeader(0x9206),
		ServerIP:       "ftp.example.com",
		Port:           21,
		Username:       "user",
		Password:       "secret",
		UploadPath:     "/upload/20261108",
		LogicChannelID: 1,
		StartTime:      &start,
		EndTime:        &end,
		AlarmSign:      0x0000000100000002,
		MediaType:      2,
		StreamType:     1,
		StorageType:    1,
		TaskCondition:  0x07,
	}
	decoded := &Msg9206{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.ServerIP, decoded.ServerIP)
	require.Equal(t, msg.Port, decoded.Port)
	require.Equal(t, msg.Username, decoded.Username)
	require.Equal(t, msg.Password, decoded.Password)
	require.Equal(t, msg.UploadPath, decoded.UploadPath)
	require.Equal(t, msg.LogicChannelID, decoded.LogicChannelID)
	require.Equal(t, hex.FormatTime(start), hex.FormatTime(*decoded.StartTime))
	require.Equal(t, hex.FormatTime(end), hex.FormatTime(*decoded.EndTime))
	require.Equal(t, msg.AlarmSign, decoded.AlarmSign)
	require.Equal(t, msg.MediaType, decoded.MediaType)
	require.Equal(t, msg.StreamType, decoded.StreamType)
	require.Equal(t, msg.StorageType, decoded.StorageType)
	require.Equal(t, msg.TaskCondition, decoded.TaskCondition)
}

func TestMsg1206RoundTrip(t *testing.T) {
	msg := &Msg1206{
		Header:             genMsgHeader(0x1206),
		AnswerSerialNumber: 42,
		Result:             0,
	}
	decoded := &Msg1206{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.AnswerSerialNumber, decoded.AnswerSerialNumber)
	require.Equal(t, msg.Result, decoded.Result)
}

func TestMsg1078SignalingDecodeRejectsMalformedBody(t *testing.T) {
	header := genMsgHeader(0x9101)
	require.ErrorIs(t, (&Msg9101{}).Decode(&PacketData{Header: header, Body: []byte{}}), ErrDecodeMsg)
	require.ErrorIs(t, (&Msg9101{}).Decode(&PacketData{Header: header, Body: []byte{5, 'a'}}), ErrDecodeMsg)
	require.ErrorIs(t, (&Msg9102{}).Decode(&PacketData{Header: header, Body: []byte{1, 2, 3}}), ErrDecodeMsg)
	require.ErrorIs(t, (&Msg9105{}).Decode(&PacketData{Header: header, Body: []byte{1}}), ErrDecodeMsg)
	require.ErrorIs(t, (&Msg9201{}).Decode(&PacketData{Header: header, Body: []byte{1, 'a'}}), ErrDecodeMsg)
	require.ErrorIs(t, (&Msg9202{}).Decode(&PacketData{Header: header, Body: []byte{1, 2, 3, 4, 5, 6, 7, 8}}), ErrDecodeMsg)
	require.ErrorIs(t, (&Msg9206{}).Decode(&PacketData{Header: header, Body: []byte{1, 'a'}}), ErrDecodeMsg)
	require.ErrorIs(t, (&Msg1206{}).Decode(&PacketData{Header: header, Body: []byte{1, 2}}), ErrDecodeMsg)
}

func TestMsg9206EncodeRejectsOversizedField(t *testing.T) {
	msg := &Msg9206{Header: genMsgHeader(0x9206)}
	for i := 0; i < 256; i++ {
		msg.ServerIP += "a"
	}
	_, err := msg.Encode()
	require.ErrorIs(t, err, ErrEncodeMsg)
}

// 对七条 1078 信令做编解码往返 + 规范字节 golden 断言。
func TestMsg1078EncodeGoldenBytes(t *testing.T) {
	cases := []struct {
		name    string
		msg     JT808Msg
		wantHex string
	}{
		{
			name: "9101",
			msg: &Msg9101{
				Header:         genMsgHeader(0x9101),
				ServerIP:       "192.168.1.1",
				TCPPort:        0x238D,
				UDPPort:        0x238E,
				LogicChannelID: 1,
				DataType:       0,
				StreamType:     1,
			},
			wantHex: "0b3139322e3136382e312e31238d238e010001",
		},
		{
			name: "9102",
			msg: &Msg9102{
				Header:         genMsgHeader(0x9102),
				LogicChannelID: 2,
				ControlCmd:     1,
				CloseMediaType: 2,
				StreamType:     0,
			},
			wantHex: "02010200",
		},
		{
			name: "9105",
			msg: &Msg9105{
				Header:         genMsgHeader(0x9105),
				LogicChannelID: 1,
				PacketLossRate: 30,
			},
			wantHex: "011e",
		},
		{
			name: "1206",
			msg: &Msg1206{
				Header:             genMsgHeader(0x1206),
				AnswerSerialNumber: 32,
				Result:             1,
			},
			wantHex: "002001",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := tc.msg.Encode()
			require.NoError(t, err)
			body := encoded[len(encoded)-int(tc.msg.GetHeader().Attr.BodyLength):]
			require.Equal(t, tc.wantHex, stdhex.EncodeToString(body))
		})
	}
}

func TestMsg9101BoundaryValues(t *testing.T) {
	maxIP := strings.Repeat("a", 255)
	msg := &Msg9101{
		Header:         genMsgHeader(0x9101),
		ServerIP:       maxIP,
		TCPPort:        65535,
		UDPPort:        0,
		LogicChannelID: 255,
		DataType:       255,
		StreamType:     255,
	}
	decoded := &Msg9101{}
	roundTrip(t, msg, decoded)
	require.Equal(t, maxIP, decoded.ServerIP)
	require.Equal(t, uint8(255), decoded.ServerIPLength)
	require.Equal(t, uint16(65535), decoded.TCPPort)
	require.Equal(t, uint16(0), decoded.UDPPort)
	require.Equal(t, uint8(255), decoded.LogicChannelID)
	require.Equal(t, uint8(255), decoded.DataType)
	require.Equal(t, uint8(255), decoded.StreamType)

	// 空 IP 合法（长度字段为 0）。
	empty := &Msg9101{Header: genMsgHeader(0x9101)}
	decoded = &Msg9101{}
	roundTrip(t, empty, decoded)
	require.Empty(t, decoded.ServerIP)
	require.Equal(t, uint8(0), decoded.ServerIPLength)

	_, err := (&Msg9101{Header: genMsgHeader(0x9101), ServerIP: strings.Repeat("a", 256)}).Encode()
	require.ErrorIs(t, err, ErrEncodeMsg)
}

func TestMsg9102And9105BoundaryValues(t *testing.T) {
	control := &Msg9102{Header: genMsgHeader(0x9102), LogicChannelID: 255, ControlCmd: 255, CloseMediaType: 255, StreamType: 255}
	decodedControl := &Msg9102{}
	roundTrip(t, control, decodedControl)
	require.Equal(t, uint8(255), decodedControl.LogicChannelID)
	require.Equal(t, uint8(255), decodedControl.ControlCmd)
	require.Equal(t, uint8(255), decodedControl.CloseMediaType)
	require.Equal(t, uint8(255), decodedControl.StreamType)

	status := &Msg9105{Header: genMsgHeader(0x9105), LogicChannelID: 0, PacketLossRate: 255}
	decodedStatus := &Msg9105{}
	roundTrip(t, status, decodedStatus)
	require.Equal(t, uint8(0), decodedStatus.LogicChannelID)
	require.Equal(t, uint8(255), decodedStatus.PacketLossRate)
}

func TestMsg9201BoundaryValues(t *testing.T) {
	maxIP := strings.Repeat("b", 255)
	msg := &Msg9201{
		Header:         genMsgHeader(0x9201),
		ServerIP:       maxIP,
		TCPPort:        65535,
		UDPPort:        65535,
		LogicChannelID: 255,
		MediaType:      255,
		StreamType:     255,
		StorageType:    255,
		PlaybackMode:   255,
		PlaybackSpeed:  255,
	}
	decoded := &Msg9201{}
	roundTrip(t, msg, decoded)
	require.Equal(t, maxIP, decoded.ServerIP)
	require.Equal(t, uint16(65535), decoded.TCPPort)
	require.Equal(t, uint16(65535), decoded.UDPPort)
	require.Equal(t, uint8(255), decoded.LogicChannelID)
	require.Equal(t, uint8(255), decoded.MediaType)
	require.Equal(t, uint8(255), decoded.StreamType)
	require.Equal(t, uint8(255), decoded.StorageType)
	require.Equal(t, uint8(255), decoded.PlaybackMode)
	require.Equal(t, uint8(255), decoded.PlaybackSpeed)
	require.Nil(t, decoded.StartTime, "zero BCD time must decode as nil")
	require.Nil(t, decoded.EndTime)

	_, err := (&Msg9201{Header: genMsgHeader(0x9201), ServerIP: strings.Repeat("b", 256)}).Encode()
	require.ErrorIs(t, err, ErrEncodeMsg)
}

func TestMsg9202BoundaryValues(t *testing.T) {
	msg := &Msg9202{
		Header:         genMsgHeader(0x9202),
		LogicChannelID: 255,
		PlayControl:    255,
		PlaySpeed:      255,
	}
	decoded := &Msg9202{}
	roundTrip(t, msg, decoded)
	require.Equal(t, uint8(255), decoded.LogicChannelID)
	require.Equal(t, uint8(255), decoded.PlayControl)
	require.Equal(t, uint8(255), decoded.PlaySpeed)
	require.Nil(t, decoded.DragPosition, "zero BCD drag position must decode as nil")
}

func TestMsg9206BoundaryValues(t *testing.T) {
	maxText := strings.Repeat("c", 255)
	msg := &Msg9206{
		Header:         genMsgHeader(0x9206),
		ServerIP:       maxText,
		Port:           65535,
		Username:       maxText,
		Password:       maxText,
		UploadPath:     maxText,
		LogicChannelID: 255,
		AlarmSign:      ^uint64(0),
		MediaType:      255,
		StreamType:     255,
		StorageType:    255,
		TaskCondition:  255,
	}
	decoded := &Msg9206{}
	roundTrip(t, msg, decoded)
	require.Equal(t, maxText, decoded.ServerIP)
	require.Equal(t, uint8(255), decoded.ServerIPLength)
	require.Equal(t, uint16(65535), decoded.Port)
	require.Equal(t, maxText, decoded.Username)
	require.Equal(t, uint8(255), decoded.UsernameLength)
	require.Equal(t, maxText, decoded.Password)
	require.Equal(t, uint8(255), decoded.PasswordLength)
	require.Equal(t, maxText, decoded.UploadPath)
	require.Equal(t, uint8(255), decoded.UploadPathLength)
	require.Equal(t, uint8(255), decoded.LogicChannelID)
	require.Equal(t, ^uint64(0), decoded.AlarmSign)
	require.Equal(t, uint8(255), decoded.MediaType)
	require.Equal(t, uint8(255), decoded.StreamType)
	require.Equal(t, uint8(255), decoded.StorageType)
	require.Equal(t, uint8(255), decoded.TaskCondition)
	require.Nil(t, decoded.StartTime)
	require.Nil(t, decoded.EndTime)

	oversized := strings.Repeat("c", 256)
	for _, tc := range []struct {
		name string
		msg  *Msg9206
	}{
		{"server ip", &Msg9206{Header: genMsgHeader(0x9206), ServerIP: oversized}},
		{"username", &Msg9206{Header: genMsgHeader(0x9206), Username: oversized}},
		{"password", &Msg9206{Header: genMsgHeader(0x9206), Password: oversized}},
		{"upload path", &Msg9206{Header: genMsgHeader(0x9206), UploadPath: oversized}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.msg.Encode()
			require.ErrorIs(t, err, ErrEncodeMsg)
		})
	}
}

func TestMsg1206BoundaryValues(t *testing.T) {
	for _, serial := range []uint16{0, 65535} {
		msg := &Msg1206{Header: genMsgHeader(0x1206), AnswerSerialNumber: serial, Result: 255}
		decoded := &Msg1206{}
		roundTrip(t, msg, decoded)
		require.Equal(t, serial, decoded.AnswerSerialNumber)
		require.Equal(t, uint8(255), decoded.Result)
	}
}

// 任一长度截断的报文体都必须返回 ErrDecodeMsg，且不能 panic。
func TestMsg1078DecodeTruncatedBodiesReturnError(t *testing.T) {
	cases := []struct {
		name      string
		msg       JT808Msg
		newTarget func() JT808Msg
	}{
		{
			name: "9101",
			msg: &Msg9101{Header: genMsgHeader(0x9101), ServerIP: "10.0.0.1", TCPPort: 7100,
				UDPPort: 7200, LogicChannelID: 1, DataType: 0, StreamType: 1},
			newTarget: func() JT808Msg { return &Msg9101{} },
		},
		{
			name: "9102",
			msg: &Msg9102{Header: genMsgHeader(0x9102), LogicChannelID: 1, ControlCmd: 1,
				CloseMediaType: 0, StreamType: 1},
			newTarget: func() JT808Msg { return &Msg9102{} },
		},
		{
			name:      "9105",
			msg:       &Msg9105{Header: genMsgHeader(0x9105), LogicChannelID: 1, PacketLossRate: 3},
			newTarget: func() JT808Msg { return &Msg9105{} },
		},
		{
			name: "9201",
			msg: &Msg9201{Header: genMsgHeader(0x9201), ServerIP: "10.0.0.1", TCPPort: 7200,
				UDPPort: 7201, LogicChannelID: 1, MediaType: 2, StreamType: 1},
			newTarget: func() JT808Msg { return &Msg9201{} },
		},
		{
			name: "9202",
			msg: &Msg9202{Header: genMsgHeader(0x9202), LogicChannelID: 1, PlayControl: 1,
				PlaySpeed: 0},
			newTarget: func() JT808Msg { return &Msg9202{} },
		},
		{
			name: "9206",
			msg: &Msg9206{Header: genMsgHeader(0x9206), ServerIP: "ftp.example.com", Port: 21,
				Username: "user", Password: "secret", UploadPath: "/upload"},
			newTarget: func() JT808Msg { return &Msg9206{} },
		},
		{
			name:      "1206",
			msg:       &Msg1206{Header: genMsgHeader(0x1206), AnswerSerialNumber: 42, Result: 0},
			newTarget: func() JT808Msg { return &Msg1206{} },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := tc.msg.Encode()
			require.NoError(t, err)
			body := encoded[len(encoded)-int(tc.msg.GetHeader().Attr.BodyLength):]
			require.NotEmpty(t, body)
			for n := 0; n < len(body); n++ {
				require.NotPanics(t, func() {
					err := tc.newTarget().Decode(&PacketData{Header: tc.msg.GetHeader(), Body: body[:n]})
					require.ErrorIs(t, err, ErrDecodeMsg, "%s truncated to %d/%d bytes", tc.name, n, len(body))
				})
			}
		})
	}
}

func TestMsg1078DecodeNilPacketReturnsError(t *testing.T) {
	decoders := []func() JT808Msg{
		func() JT808Msg { return &Msg9101{} },
		func() JT808Msg { return &Msg9102{} },
		func() JT808Msg { return &Msg9105{} },
		func() JT808Msg { return &Msg9201{} },
		func() JT808Msg { return &Msg9202{} },
		func() JT808Msg { return &Msg9206{} },
		func() JT808Msg { return &Msg1206{} },
	}
	for _, newTarget := range decoders {
		require.NotPanics(t, func() {
			require.ErrorIs(t, newTarget().Decode(nil), ErrDecodeMsg)
		})
	}
}
