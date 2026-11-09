package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func genMsgHeader(msgID uint16) *MsgHeader {
	return genMsgHeaderVersion(msgID, Version2019)
}

// genMsgHeaderVersion 构造指定协议版本的消息头，便于按版本校验编解码差异。
func genMsgHeaderVersion(msgID uint16, version VersionType) *MsgHeader {
	header := &MsgHeader{
		MsgID: msgID,
		Attr: &MsgBodyAttr{
			Encryption:           uint8(EncryptionNone),
			PacketFragmented:     0,
			VersionSign:          versionDecode(version),
			Extra:                0,
			EncryptionDesc:       EncryptionNone,
			PacketFragmentedDesc: PacketFragmentedFalse,
			VersionDesc:          version,
		},
		ProtocolVersion: versionDecode(version),
		PhoneNumber:     "12345678901234567890",
		SerialNumber:    1,
		Frag:            nil,
	}
	if version != Version2019 {
		header.PhoneNumber = "123456789012"
	}
	return header
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
