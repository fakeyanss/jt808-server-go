package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func genMsgHeader(msgID uint16) *MsgHeader {
	return &MsgHeader{
		MsgID: msgID,
		Attr: &MsgBodyAttr{
			Encryption:           uint8(EncryptionNone),
			PacketFragmented:     0,
			VersionSign:          1,
			Extra:                0,
			EncryptionDesc:       EncryptionNone,
			PacketFragmentedDesc: PacketFragmentedFalse,
			VersionDesc:          Version2019,
		},
		ProtocolVersion: 1,
		PhoneNumber:     "12345678901234567890",
		SerialNumber:    1,
		Frag:            nil,
	}
}

// roundTrip encodes msg and decodes the body into target.
func roundTrip(t *testing.T, msg JT808Msg, target JT808Msg) *PacketData {
	t.Helper()
	encoded, err := msg.Encode()
	require.NoError(t, err)
	bodyLength := int(msg.GetHeader().Attr.BodyLength)
	pkt := &PacketData{Header: msg.GetHeader(), Body: encoded[len(encoded)-bodyLength:]}
	require.NoError(t, target.Decode(pkt))
	return pkt
}
