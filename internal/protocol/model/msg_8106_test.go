package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg8106EncodeDecode(t *testing.T) {
	msg := &Msg8106{Header: genMsgHeader(0x8106), ParamIDs: []uint32{1, 0x12345678}}
	encoded, err := msg.Encode()
	require.NoError(t, err)
	decoded := &Msg8106{}
	require.NoError(t, decoded.Decode(&PacketData{Header: msg.Header, Body: encoded[17:]}))
	require.Equal(t, msg.ParamIDs, decoded.ParamIDs)
	require.Error(t, decoded.Decode(&PacketData{Header: msg.Header, Body: []byte{2, 0, 0, 0, 1}}))
}
