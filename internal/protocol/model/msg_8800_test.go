package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg8800RoundTrip(t *testing.T) {
	ack := &Msg8800{Header: genMsgHeader(0x8800), MultiMediaID: 91, RetransmitIDs: []uint16{2, 9}}
	decoded := &Msg8800{}
	roundTrip(t, ack, decoded)
	require.Equal(t, ack.RetransmitIDs, decoded.RetransmitIDs)
}
