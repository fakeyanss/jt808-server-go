package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg8801RoundTrip(t *testing.T) {
	capture := &Msg8801{Header: genMsgHeader(0x8801), ChannelID: 1, Command: 3, Interval: 2, Save: 1, Resolution: 2, Quality: 5, Brightness: 10, Contrast: 20, Saturation: 30, Chroma: 40}
	decoded := &Msg8801{}
	roundTrip(t, capture, decoded)
	require.Equal(t, capture.Command, decoded.Command)
	require.Equal(t, capture.Chroma, decoded.Chroma)
}

// 标准 4.8 规定通道号 0x00~0x25 合法，边界必须放行且仅拒绝 0x26 及以上。
func TestMsg8801ChannelBoundaries(t *testing.T) {
	for _, channel := range []uint8{0x00, 0x25} {
		capture := &Msg8801{Header: genMsgHeader(0x8801), ChannelID: channel, Command: 1, Quality: 1}
		decoded := &Msg8801{}
		roundTrip(t, capture, decoded)
		require.Equal(t, channel, decoded.ChannelID)
	}

	invalid := &Msg8801{Header: genMsgHeader(0x8801), ChannelID: 0x26, Command: 1, Quality: 1}
	_, err := invalid.Encode()
	require.ErrorIs(t, err, ErrEncodeMsg)

	body := []byte{0x26, 0, 1, 0, 0, 1, 1, 1, 0, 0, 0, 0}
	err = (&Msg8801{}).Decode(&PacketData{Header: genMsgHeader(0x8801), Body: body})
	require.ErrorIs(t, err, ErrDecodeMsg)
}
