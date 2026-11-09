package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg8003EncodeDecode(t *testing.T) {
	msg := &Msg8003{
		Header:               genMsgHeader(0x8003),
		OriginalSerialNumber: 0x1234,
		PacketIDs:            []uint16{2, 5, 9},
	}
	encoded, err := msg.Encode()
	require.NoError(t, err)
	decoded := &Msg8003{}
	require.NoError(t, decoded.Decode(&PacketData{Header: msg.Header, Body: encoded[17:]}))
	require.Equal(t, uint16(0x1234), decoded.OriginalSerialNumber)
	require.Equal(t, []uint16{2, 5, 9}, decoded.PacketIDs)
	require.Equal(t, uint16(3), decoded.RetransmitCount)
}

// TestMsg8003CountWidthByVersion 校验重传包总数字段在 2013 为 BYTE、2019 为 WORD。
func TestMsg8003CountWidthByVersion(t *testing.T) {
	t.Run("2013", func(t *testing.T) {
		msg := &Msg8003{
			Header:               genMsgHeaderVersion(0x8003, Version2013),
			OriginalSerialNumber: 0x1234,
			PacketIDs:            []uint16{2, 5, 9},
		}
		decoded := &Msg8003{}
		pkt := roundTrip(t, msg, decoded)
		// 原流水号(2) + 重传包总数(1) + ID 列表(6)
		require.Len(t, pkt.Body, 2+1+6)
		require.Equal(t, []uint16{2, 5, 9}, decoded.PacketIDs)
		require.Equal(t, uint16(3), decoded.RetransmitCount)
	})

	t.Run("2019", func(t *testing.T) {
		msg := &Msg8003{
			Header:               genMsgHeaderVersion(0x8003, Version2019),
			OriginalSerialNumber: 0x1234,
			PacketIDs:            []uint16{2, 5, 9},
		}
		decoded := &Msg8003{}
		pkt := roundTrip(t, msg, decoded)
		// 原流水号(2) + 重传包总数(2) + ID 列表(6)
		require.Len(t, pkt.Body, 2+2+6)
		require.Equal(t, []uint16{2, 5, 9}, decoded.PacketIDs)
		require.Equal(t, uint16(3), decoded.RetransmitCount)
	})
}

// TestMsg8003RejectsShortBody 校验各版本最小消息体长度与声明长度不一致。
func TestMsg8003RejectsShortBody(t *testing.T) {
	header2019 := genMsgHeaderVersion(0x8003, Version2019)
	header2013 := genMsgHeaderVersion(0x8003, Version2013)

	require.Error(t, (&Msg8003{}).Decode(&PacketData{Header: header2019, Body: []byte{0, 1, 0}}))
	require.Error(t, (&Msg8003{}).Decode(&PacketData{Header: header2013, Body: []byte{0, 1}}))

	// 声明重传 2 个分包但只提供 1 个 ID
	require.Error(t, (&Msg8003{}).Decode(&PacketData{Header: header2013, Body: []byte{0, 1, 2, 0, 3}}))
	require.Error(t, (&Msg8003{}).Decode(&PacketData{Header: header2019, Body: []byte{0, 1, 0, 2, 0, 3}}))

	// 2013 版本重传包数量超过 255 时禁止编码
	ids := make([]uint16, 256)
	_, err := (&Msg8003{Header: header2013, PacketIDs: ids}).Encode()
	require.Error(t, err)
}
