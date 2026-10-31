package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg0608EmptyRoundTrip(t *testing.T) {
	response := &Msg0608{Header: genMsgHeader(0x0608), QueryType: 4}
	decoded := &Msg0608{}
	roundTrip(t, response, decoded)
	require.Zero(t, decoded.Count)
}

func TestMsg0608RoundTripWithRecords(t *testing.T) {
	resp := &Msg0608{
		Header:    genMsgHeader(0x0608),
		QueryType: 1,
		Circles: []CircleArea{
			{ID: 1, Attr: 0, Latitude: 31000000, Longitude: 121000000, Radius: 500, Name: "圆1"},
			{ID: 2, Attr: AreaAttrSpeed, Latitude: 32000000, Longitude: 122000000, Radius: 300, MaxSpeed: 500, Duration: 5, Name: "圆2"},
		},
	}
	decoded := &Msg0608{}
	roundTrip(t, resp, decoded)
	require.Equal(t, resp.QueryType, decoded.QueryType)
	require.Equal(t, uint32(2), decoded.Count)
	require.Equal(t, resp.Circles, decoded.Circles)
}
