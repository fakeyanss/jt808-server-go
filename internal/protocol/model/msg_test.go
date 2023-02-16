package model

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
