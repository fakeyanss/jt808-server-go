package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg8608RoundTrip(t *testing.T) {
	query := &Msg8608{Header: genMsgHeader(0x8608), QueryType: 4, IDs: []uint32{10, 20}}
	decoded := &Msg8608{}
	roundTrip(t, query, decoded)
	require.Equal(t, query.QueryType, decoded.QueryType)
	require.Equal(t, query.IDs, decoded.IDs)
}
