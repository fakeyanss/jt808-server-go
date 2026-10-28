package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg8203EncodeDecode(t *testing.T) {
	msg := &Msg8203{Header: genMsgHeader(0x8203), AlarmSerial: 0x1234, AlarmType: 0x10000001}
	encoded, err := msg.Encode()
	require.NoError(t, err)
	decoded := &Msg8203{}
	require.NoError(t, decoded.Decode(&PacketData{Header: msg.Header, Body: encoded[17:]}))
	require.Equal(t, msg.AlarmSerial, decoded.AlarmSerial)
	require.Equal(t, msg.AlarmType, decoded.AlarmType)
	require.Error(t, decoded.Decode(&PacketData{Header: msg.Header, Body: []byte{0, 1}}))
}

func TestMsg8204EmptyBody(t *testing.T) {
	msg := &Msg8204{}
	require.NoError(t, msg.Decode(&PacketData{Header: genMsgHeader(0x8204)}))
	require.Error(t, msg.Decode(&PacketData{Header: genMsgHeader(0x8204), Body: []byte{1}}))
}
