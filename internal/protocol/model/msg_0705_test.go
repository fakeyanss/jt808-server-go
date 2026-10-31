package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg0705RoundTrip(t *testing.T) {
	can := &Msg0705{Header: genMsgHeader(0x0705), FirstTime: "1234560100", Items: []CANDataItem{{CANID: 0x60000123, Data: [8]byte{1, 2, 3}}}}
	decoded := &Msg0705{}
	roundTrip(t, can, decoded)
	require.Equal(t, can.Items, decoded.Items)
	require.Equal(t, can.FirstTime, decoded.FirstTime)
}
