package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg0201EncodeDecode(t *testing.T) {
	header := genMsgHeader(0x0201)
	msg := &Msg0201{Header: header, AnswerSerialNumber: 17, Location: &Msg0200{AlarmSign: 3, StatusSign: 5, Latitude: 31123456, Longitude: 121123456, Altitude: 8, Speed: 42, Direction: 90, Time: "261028204500"}}
	encoded, err := msg.Encode()
	require.NoError(t, err)
	decoded := &Msg0201{}
	require.NoError(t, decoded.Decode(&PacketData{Header: header, Body: encoded[17:]}))
	require.Equal(t, msg.AnswerSerialNumber, decoded.AnswerSerialNumber)
	require.Equal(t, msg.Location.AlarmSign, decoded.Location.AlarmSign)
	require.Equal(t, msg.Location.StatusSign, decoded.Location.StatusSign)
	require.Equal(t, msg.Location.Latitude, decoded.Location.Latitude)
	require.Equal(t, msg.Location.Longitude, decoded.Location.Longitude)
	require.Equal(t, msg.Location.Altitude, decoded.Location.Altitude)
	require.Equal(t, msg.Location.Speed, decoded.Location.Speed)
	require.Equal(t, msg.Location.Direction, decoded.Location.Direction)
	require.Equal(t, msg.Location.Time, decoded.Location.Time)
	require.Error(t, decoded.Decode(&PacketData{Header: header, Body: make([]byte, 29)}))
}

func TestMsg8201RequiresEmptyBody(t *testing.T) {
	msg := &Msg8201{}
	require.NoError(t, msg.Decode(&PacketData{Header: genMsgHeader(0x8201)}))
	require.Error(t, msg.Decode(&PacketData{Header: genMsgHeader(0x8201), Body: []byte{1}}))
}
