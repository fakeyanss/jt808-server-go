package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg8106EncodeDecode(t *testing.T) {
	msg := &Msg8106{Header: genMsgHeader(0x8106), ParamIDs: []uint32{1, 0x12345678}}
	encoded, err := msg.Encode()
	require.NoError(t, err)
	decoded := &Msg8106{}
	require.NoError(t, decoded.Decode(&PacketData{Header: msg.Header, Body: encoded[17:]}))
	require.Equal(t, msg.ParamIDs, decoded.ParamIDs)
	require.Error(t, decoded.Decode(&PacketData{Header: msg.Header, Body: []byte{2, 0, 0, 0, 1}}))
}

func TestMsg0107EncodeDecodeVersions(t *testing.T) {
	for _, version := range []VersionType{Version2011, Version2013, Version2019} {
		header := genMsgHeader(0x0107)
		header.Attr.VersionDesc = version
		if version != Version2019 {
			header.PhoneNumber = "123456789012"
		}
		modelID, terminalID := "model", "1234567"
		if version == Version2019 {
			terminalID = "terminal-id"
		}
		msg := &Msg0107{Header: header, TerminalType: 1, ManufacturerID: "ABC01", TerminalModel: modelID, TerminalID: terminalID, ICCID: "89860000000000000000", HardwareVersion: "hw", FirmwareVersion: "fw", GNSSProperties: 3, CommunicationProperties: 4}
		encoded, err := msg.Encode()
		require.NoError(t, err)
		decoded := &Msg0107{}
		header.MsgID = 0x0107
		headerLen := 12
		if version == Version2019 {
			headerLen = 17
		}
		require.NoError(t, decoded.Decode(&PacketData{Header: header, Body: encoded[headerLen:]}))
		require.Equal(t, msg.ManufacturerID, decoded.ManufacturerID)
		require.Equal(t, terminalID, decoded.TerminalID)
		require.Equal(t, msg.ICCID, decoded.ICCID)
		require.Equal(t, msg.FirmwareVersion, decoded.FirmwareVersion)
	}
}
