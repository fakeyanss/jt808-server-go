package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg8105EncodeDecode(t *testing.T) {
	msg := &Msg8105{Header: genMsgHeader(0x8105), CommandWord: 2, CommandParam: "example.com;7611;TOKEN"}
	encoded, err := msg.Encode()
	require.NoError(t, err)
	decoded := &Msg8105{}
	require.NoError(t, decoded.Decode(&PacketData{Header: msg.Header, Body: encoded[len(encoded)-len([]byte(msg.CommandParam))-1:]}))
	require.Equal(t, msg.CommandWord, decoded.CommandWord)
	require.Equal(t, msg.CommandParam, decoded.CommandParam)
	require.Error(t, (&Msg8105{}).Decode(&PacketData{Body: nil}))
}
