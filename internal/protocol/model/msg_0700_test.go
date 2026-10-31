package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg0700RoundTrip(t *testing.T) {
	response := &Msg0700{Header: genMsgHeader(0x0700), AnswerSerial: 7, Command: 3, Data: []byte{4, 5}}
	decoded := &Msg0700{}
	roundTrip(t, response, decoded)
	require.Equal(t, response.AnswerSerial, decoded.AnswerSerial)
	require.Equal(t, response.Data, decoded.Data)
}

func TestMsg0700TypedDecode(t *testing.T) {
	infoResp := &RecorderInfoResponse{
		Magic: "GBT19056", StandardVersion: "21", RecorderTime: "261029120000",
		RecorderID: make([]byte, 35), PlateNo: "京A12345", PlateCategory: "小型汽车",
		VIN: "LSVNV2182E2100001", SerialNo: "000000000001", PulseCoeff: 100, InstallTime: "230101000000",
	}
	data, err := infoResp.Encode()
	require.NoError(t, err)
	body := []byte{0x00, 0x01, RecorderCmdCollectInfo}
	body = append(body, data...)
	resp := &Msg0700{}
	require.NoError(t, resp.Decode(&PacketData{Header: genMsgHeader(0x0700), Body: body}))
	require.Equal(t, uint16(1), resp.AnswerSerial)
	require.Equal(t, RecorderCmdCollectInfo, resp.Command)
	require.IsType(t, &RecorderInfoResponse{}, resp.DataBlock)
	require.Equal(t, infoResp.PlateNo, resp.DataBlock.(*RecorderInfoResponse).PlateNo)
}
