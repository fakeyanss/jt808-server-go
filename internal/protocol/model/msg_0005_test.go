package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg0005EncodeDecode(t *testing.T) {
	msg := &Msg0005{Header: genMsgHeader(0x0005), OriginalSerialNumber: 0x1234, PacketIDs: []uint16{2, 5, 9}}
	encoded, err := msg.Encode()
	require.NoError(t, err)
	decoded := &Msg0005{}
	require.NoError(t, decoded.Decode(&PacketData{Header: msg.Header, Body: encoded[17:]}))
	require.Equal(t, uint16(0x1234), decoded.OriginalSerialNumber)
	require.Equal(t, []uint16{2, 5, 9}, decoded.PacketIDs)
	require.Equal(t, uint16(3), decoded.RetransmitCount)
	require.Error(t, decoded.Decode(&PacketData{Header: msg.Header, Body: []byte{0, 1, 0, 2, 0}}))
}
