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

// 截断的条目、零媒体 ID 与越界媒体类型都必须拒绝解码。
func TestMsg0802RejectsIllegalData(t *testing.T) {
	valid := &Msg0802{
		Header:       genMsgHeader(0x0802),
		AnswerSerial: 7,
		Items: []MediaSearchItem{{
			MediaID: 10, MediaType: 0, ChannelID: 1,
			Location: Msg0200{Time: "261028000000"},
		}},
	}
	encoded, err := valid.Encode()
	require.NoError(t, err)
	body := encoded[len(encoded)-int(valid.Header.Attr.BodyLength):]

	require.ErrorIs(t, (&Msg0802{}).Decode(&PacketData{Header: genMsgHeader(0x0802), Body: body[:len(body)-1]}), ErrDecodeMsg)

	zeroMediaID := append([]byte(nil), body...)
	copy(zeroMediaID[4:8], []byte{0, 0, 0, 0})
	require.ErrorIs(t, (&Msg0802{}).Decode(&PacketData{Header: genMsgHeader(0x0802), Body: zeroMediaID}), ErrDecodeMsg)

	illegalType := append([]byte(nil), body...)
	illegalType[8] = 3
	require.ErrorIs(t, (&Msg0802{}).Decode(&PacketData{Header: genMsgHeader(0x0802), Body: illegalType}), ErrDecodeMsg)
}
