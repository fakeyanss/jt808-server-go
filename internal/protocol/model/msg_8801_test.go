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
