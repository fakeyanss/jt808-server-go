package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg0802RoundTrip(t *testing.T) {
	result := &Msg0802{
		Header:       genMsgHeader(0x0802),
		AnswerSerial: 7,
		Items: []MediaSearchItem{{
			MediaID: 10, MediaType: 2, ChannelID: 1, EventID: 3,
			Location: Msg0200{Latitude: 31000000, Longitude: 121000000, Time: "261028000000"},
		}},
	}
	decoded := &Msg0802{}
	roundTrip(t, result, decoded)
	require.Equal(t, result.Items[0].MediaID, decoded.Items[0].MediaID)
	require.Equal(t, result.Items[0].Location.Latitude, decoded.Items[0].Location.Latitude)
}
