package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg8802RoundTrip(t *testing.T) {
	search := &Msg8802{Header: genMsgHeader(0x8802), MediaType: 2, ChannelID: 1, EventID: 3, StartTime: "261028000000", EndTime: "261028235959"}
	decoded := &Msg8802{}
	roundTrip(t, search, decoded)
	require.Equal(t, search.StartTime, decoded.StartTime)
	require.Equal(t, search.EndTime, decoded.EndTime)
}
