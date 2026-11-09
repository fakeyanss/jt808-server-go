package model

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// TestMsg8300VersionLayouts 校验文本类型字段只在 2019 版本存在。
func TestMsg8300VersionLayouts(t *testing.T) {
	const text = "通知"
	gbk := hex.WriteGBK(nil, text)

	t.Run("2019", func(t *testing.T) {
		msg := &Msg8300{Header: genMsgHeaderVersion(0x8300, Version2019), Flag: 1, TextType: 2, Text: text}
		decoded := &Msg8300{}
		pkt := roundTrip(t, msg, decoded)
		// 标志(1) + 文本类型(1) + GBK 文本
		require.Len(t, pkt.Body, 2+len(gbk))
		require.Equal(t, uint8(1), decoded.Flag)
		require.Equal(t, uint8(2), decoded.TextType)
		require.Equal(t, text, decoded.Text)
	})

	t.Run("2013", func(t *testing.T) {
		msg := &Msg8300{Header: genMsgHeaderVersion(0x8300, Version2013), Flag: 1, TextType: 2, Text: text}
		decoded := &Msg8300{}
		pkt := roundTrip(t, msg, decoded)
		// 标志(1) + GBK 文本，没有文本类型字段
		require.Len(t, pkt.Body, 1+len(gbk))
		require.Equal(t, uint8(1), decoded.Flag)
		require.Equal(t, uint8(0), decoded.TextType)
		require.Equal(t, text, decoded.Text)
	})
}

// TestMsg8300RejectsShortBody 校验各版本最小消息体长度。
func TestMsg8300RejectsShortBody(t *testing.T) {
	header2019 := genMsgHeaderVersion(0x8300, Version2019)
	header2013 := genMsgHeaderVersion(0x8300, Version2013)

	require.Error(t, (&Msg8300{}).Decode(&PacketData{Header: header2019, Body: nil}))
	require.Error(t, (&Msg8300{}).Decode(&PacketData{Header: header2019, Body: []byte{1}}))
	require.NoError(t, (&Msg8300{}).Decode(&PacketData{Header: header2019, Body: []byte{1, 2}}))

	require.Error(t, (&Msg8300{}).Decode(&PacketData{Header: header2013, Body: nil}))
	require.NoError(t, (&Msg8300{}).Decode(&PacketData{Header: header2013, Body: []byte{1}}))
}
