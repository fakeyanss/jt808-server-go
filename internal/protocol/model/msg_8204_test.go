package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg8204EmptyBody(t *testing.T) {
	msg := &Msg8204{}
	require.NoError(t, msg.Decode(&PacketData{Header: genMsgHeader(0x8204)}))
	require.Error(t, msg.Decode(&PacketData{Header: genMsgHeader(0x8204), Body: []byte{1}}))
}
