package model

import (
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
