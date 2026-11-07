package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg1210RoundTrip(t *testing.T) {
	msg := &Msg1210{
		Header:     genMsgHeader(0x1210),
		TerminalID: "ABC1234",
		AlarmSign:  []byte{0x01, 0x02, 0x26, 0x10, 0x29, 0x12, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x00},
		AlarmNo:    "ALARM-000000000000000000000001",
		InfoType:   0,
		Attachments: []AlarmAttachmentEntry{
			{FileName: "01_64_6401_01_ALARM.jpg", FileSize: 1024},
			{FileName: "02_65_6501_01_ALARM.h264", FileSize: 2048},
		},
	}
	decoded := &Msg1210{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.TerminalID, decoded.TerminalID)
	require.Equal(t, msg.AlarmSign, decoded.AlarmSign)
	require.Equal(t, msg.AlarmNo, decoded.AlarmNo)
	require.Equal(t, uint8(2), decoded.AttachmentCount)
	require.Equal(t, msg.Attachments, decoded.Attachments)
}

func TestMsg1211RoundTrip(t *testing.T) {
	msg := &Msg1211{
		Header:   genMsgHeader(0x1211),
		FileName: "00_64_6401_01_ALARM.jpg",
		FileType: 0,
		FileSize: 4096,
	}
	decoded := &Msg1211{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.FileName, decoded.FileName)
	require.Equal(t, msg.FileType, decoded.FileType)
	require.Equal(t, msg.FileSize, decoded.FileSize)
}

func TestMsg1212RoundTrip(t *testing.T) {
	msg := &Msg1212{
		Header:   genMsgHeader(0x1212),
		FileName: "02_65_6501_01_ALARM.h264",
		FileType: 2,
		FileSize: 8192,
	}
	decoded := &Msg1212{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.FileName, decoded.FileName)
	require.Equal(t, msg.FileType, decoded.FileType)
	require.Equal(t, msg.FileSize, decoded.FileSize)
}

func TestMsg9208RoundTrip(t *testing.T) {
	msg := &Msg9208{
		Header:    genMsgHeader(0x9208),
		IP:        "127.0.0.1",
		TCPPort:   7200,
		UDPPort:   7201,
		AlarmSign: []byte{0x01, 0x02, 0x03},
		AlarmNo:   "ALARM-001",
	}
	decoded := &Msg9208{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.IP, decoded.IP)
	require.Equal(t, msg.TCPPort, decoded.TCPPort)
	require.Equal(t, msg.UDPPort, decoded.UDPPort)
	require.Len(t, decoded.AlarmSign, 16)
	require.Equal(t, msg.AlarmSign, decoded.AlarmSign[:3])
	require.Equal(t, msg.AlarmNo, decoded.AlarmNo)
}

func TestMsg9212RoundTrip(t *testing.T) {
	msg := &Msg9212{
		Header:   genMsgHeader(0x9212),
		FileName: "02_65_6501_01_ALARM.h264",
		FileType: 2,
		Result:   1,
		RetransmitPackets: []RetransmitPacket{
			{Offset: 65536, Length: 65536},
			{Offset: 262144, Length: 2048},
		},
	}
	decoded := &Msg9212{}
	roundTrip(t, msg, decoded)
	require.Equal(t, msg.FileName, decoded.FileName)
	require.Equal(t, msg.FileType, decoded.FileType)
	require.Equal(t, msg.Result, decoded.Result)
	require.Equal(t, uint8(2), decoded.RetransmitCount)
	require.Equal(t, msg.RetransmitPackets, decoded.RetransmitPackets)
}

func TestMsg9212GenOutgoingKeepsIncomingHeader(t *testing.T) {
	incoming := &Msg1212{Header: genMsgHeader(0x1212), FileName: "a.jpg", FileType: 0, FileSize: 1}
	outgoing := &Msg9212{}
	require.NoError(t, outgoing.GenOutgoing(incoming))
	require.Equal(t, uint16(0x9212), outgoing.Header.MsgID)
	require.Equal(t, uint16(0x1212), incoming.Header.MsgID)
}

func TestMsgSubiaoAttachmentDecodeRejectsTruncatedBody(t *testing.T) {
	header := genMsgHeader(0x1210)
	require.ErrorIs(t, (&Msg1210{}).Decode(&PacketData{Header: header, Body: []byte{1, 2, 3}}), ErrDecodeMsg)
	require.ErrorIs(t, (&Msg1211{}).Decode(&PacketData{Header: header, Body: []byte{0}}), ErrDecodeMsg)
	require.ErrorIs(t, (&Msg1212{}).Decode(&PacketData{Header: header, Body: []byte{4, 0x61}}), ErrDecodeMsg)
	require.ErrorIs(t, (&Msg9208{}).Decode(&PacketData{Header: header, Body: []byte{0}}), ErrDecodeMsg)
	require.ErrorIs(t, (&Msg9212{}).Decode(&PacketData{Header: header, Body: []byte{3, 0x61}}), ErrDecodeMsg)
}
