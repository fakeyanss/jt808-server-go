package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg8700RoundTrip(t *testing.T) {
	command := &MsgRecordCommand{Header: genMsgHeader(0x8700), Command: 3, Data: []byte{0x11, 0x22}}
	decoded := &MsgRecordCommand{}
	roundTrip(t, command, decoded)
	require.Equal(t, command.Command, decoded.Command)
	require.Equal(t, command.Data, decoded.Data)
}

func TestMsg8700TypedDecode(t *testing.T) {
	infoReq := &RecorderInfoRequest{StandardVersion: "21", CommTime: "261029120000", CommDeviceName: "DEV"}
	data, err := infoReq.Encode()
	require.NoError(t, err)
	body := append([]byte{RecorderCmdCollectInfo}, data...)
	cmd := &MsgRecordCommand{}
	require.NoError(t, cmd.Decode(&PacketData{Header: genMsgHeader(0x8700), Body: body}))
	require.Equal(t, RecorderCmdCollectInfo, cmd.Command)
	require.IsType(t, &RecorderInfoRequest{}, cmd.DataBlock)
	require.Equal(t, infoReq.CommDeviceName, cmd.DataBlock.(*RecorderInfoRequest).CommDeviceName)

	// Re-encode should produce the same bytes.
	encoded, err := cmd.Encode()
	require.NoError(t, err)
	headerLen := 12
	if cmd.Header.Attr.VersionDesc == Version2019 {
		headerLen = 17
	}
	require.Equal(t, body, encoded[headerLen:])
}
