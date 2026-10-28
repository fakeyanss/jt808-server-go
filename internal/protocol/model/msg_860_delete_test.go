package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg860DeleteEncodeDecode(t *testing.T) {
	msg := &Msg860Delete{Header: genMsgHeader(0x8607), IDs: []uint32{1, 0x12345678}}
	encoded, err := msg.Encode()
	require.NoError(t, err)
	decoded := &Msg860Delete{}
	require.NoError(t, decoded.Decode(&PacketData{Header: msg.Header, Body: encoded[len(encoded)-9:]}))
	require.Equal(t, msg.IDs, decoded.IDs)
	all := &Msg860Delete{Header: genMsgHeader(0x8601)}
	encoded, err = all.Encode()
	require.NoError(t, err)
	require.Equal(t, []byte{0}, encoded[len(encoded)-1:])
	_, err = (&Msg860Delete{Header: genMsgHeader(0x8601), IDs: make([]uint32, 126)}).Encode()
	require.Error(t, err)
	require.Error(t, (&Msg860Delete{}).Decode(&PacketData{Header: msg.Header, Body: []byte{1, 0}}))
}
