package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg8202EncodeDecodeTrackingAndStop(t *testing.T) {
	for _, msg := range []*Msg8202{{Header: genMsgHeader(0x8202), TimeInterval: 5, ValidityPeriod: 60}, {Header: genMsgHeader(0x8202)}} {
		encoded, err := msg.Encode()
		require.NoError(t, err)
		decoded := &Msg8202{}
		require.NoError(t, decoded.Decode(&PacketData{Header: msg.Header, Body: encoded[17:]}))
		require.Equal(t, msg.TimeInterval, decoded.TimeInterval)
		require.Equal(t, msg.ValidityPeriod, decoded.ValidityPeriod)
	}
	msg := &Msg8202{Header: genMsgHeader(0x8202), TimeInterval: 0, ValidityPeriod: 10}
	_, err := msg.Encode()
	require.NoError(t, err, "stop command omits the validity field")
	require.Error(t, (&Msg8202{}).Decode(&PacketData{Header: genMsgHeader(0x8202), Body: []byte{0, 0, 0, 0, 0, 1}}))
}
