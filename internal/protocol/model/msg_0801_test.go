package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg0801RoundTrip(t *testing.T) {
	upload := &Msg0801{Header: genMsgHeader(0x0801), MultiMediaID: 91, MultiMediaType: 0, MultiMediaContainer: 0, EventID: 1, LogicChannelID: 2, GeoAlarmBody: make([]byte, 28), FragmentData: []byte{0xff, 0xd8, 0xff}}
	decoded := &Msg0801{}
	roundTrip(t, upload, decoded)
	require.Equal(t, upload.MultiMediaID, decoded.MultiMediaID)
	require.Equal(t, upload.GeoAlarmBody, decoded.GeoAlarmBody)
	require.Equal(t, upload.FragmentData, decoded.FragmentData)
}
