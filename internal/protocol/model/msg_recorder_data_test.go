package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecorderInfoRequestRoundTrip(t *testing.T) {
	orig := &RecorderInfoRequest{
		StandardVersion: "21",
		RevisionNo:      0,
		CommTime:        "261029120000",
		CommDeviceName:  "COMM-DEVICE-001",
	}
	encoded, err := orig.Encode()
	require.NoError(t, err)
	require.Len(t, encoded, 24)

	decoded := &RecorderInfoRequest{}
	require.NoError(t, decoded.Decode(encoded))
	require.Equal(t, orig.StandardVersion, decoded.StandardVersion)
	require.Equal(t, orig.RevisionNo, decoded.RevisionNo)
	require.Equal(t, orig.CommTime, decoded.CommTime)
	require.Equal(t, orig.CommDeviceName, decoded.CommDeviceName)
}

func TestRecorderInfoResponseRoundTrip(t *testing.T) {
	orig := &RecorderInfoResponse{
		Magic:           "GBT19056",
		StandardVersion: "21",
		RevisionNo:      0,
		RecorderTime:    "261029120000",
		RecorderID:      make([]byte, 35),
		PlateNo:         "京A12345",
		PlateCategory:   "小型汽车",
		VIN:             "LSVNV2182E2100001",
		SerialNo:        "000000000001",
		PulseCoeff:      12345,
		InstallTime:     "230101000000",
	}
	for i := range orig.RecorderID {
		orig.RecorderID[i] = byte(i)
	}
	encoded, err := orig.Encode()
	require.NoError(t, err)
	require.Len(t, encoded, 112)

	decoded := &RecorderInfoResponse{}
	require.NoError(t, decoded.Decode(encoded))
	require.Equal(t, orig.Magic, decoded.Magic)
	require.Equal(t, orig.StandardVersion, decoded.StandardVersion)
	require.Equal(t, orig.RecorderTime, decoded.RecorderTime)
	require.Equal(t, orig.RecorderID, decoded.RecorderID)
	require.Equal(t, orig.PlateNo, decoded.PlateNo)
	require.Equal(t, orig.VIN, decoded.VIN)
	require.Equal(t, orig.PulseCoeff, decoded.PulseCoeff)
	require.Equal(t, orig.InstallTime, decoded.InstallTime)
}

func TestRecorderFileRequestRoundTrip(t *testing.T) {
	orig := &RecorderFileRequest{
		Items: []RecorderFileRequestItem{
			{Category: FileCategoryDrivingState, StartTime: "261029000000", EndTime: "261029235959"},
			{Category: FileCategoryDriver, StartTime: "261028000000", EndTime: "261028235959"},
		},
	}
	encoded, err := orig.Encode()
	require.NoError(t, err)
	require.Len(t, encoded, 26)

	decoded := &RecorderFileRequest{}
	require.NoError(t, decoded.Decode(encoded))
	require.Len(t, decoded.Items, 2)
	require.Equal(t, orig.Items[0].Category, decoded.Items[0].Category)
	require.Equal(t, orig.Items[0].StartTime, decoded.Items[0].StartTime)
	require.Equal(t, orig.Items[1].Category, decoded.Items[1].Category)
}

func TestRecorderFileResponseRoundTrip(t *testing.T) {
	orig := &RecorderFileResponse{
		Items: []RecorderFileResponseItem{
			{Category: FileCategoryLog, FileLength: 4, FileData: []byte{0xAA, 0xBB, 0xCC, 0xDD}},
		},
	}
	encoded, err := orig.Encode()
	require.NoError(t, err)

	decoded := &RecorderFileResponse{}
	require.NoError(t, decoded.Decode(encoded))
	require.Len(t, decoded.Items, 1)
	require.Equal(t, orig.Items[0].Category, decoded.Items[0].Category)
	require.Equal(t, orig.Items[0].FileLength, decoded.Items[0].FileLength)
	require.Equal(t, orig.Items[0].FileData, decoded.Items[0].FileData)
}

func TestRecorderParamSettingRoundTrip(t *testing.T) {
	orig := &RecorderParamSetting{
		ParamFlags:    0xFF,
		RecorderID:    make([]byte, 35),
		PlateNo:       "京B54321",
		PlateCategory: "大型汽车",
		VIN:           "LSVNV2182E2100002",
		SerialNo:      "000000000002",
		PulseCoeff:    54321,
		InstallTime:   "220615000000",
		Salt:          make([]byte, 16),
	}
	for i := range orig.RecorderID {
		orig.RecorderID[i] = byte(i + 1)
	}
	for i := range orig.Salt {
		orig.Salt[i] = byte(i + 10)
	}
	encoded, err := orig.Encode()
	require.NoError(t, err)
	require.Len(t, encoded, 113)

	decoded := &RecorderParamSetting{}
	require.NoError(t, decoded.Decode(encoded))
	require.Equal(t, orig.ParamFlags, decoded.ParamFlags)
	require.Equal(t, orig.RecorderID, decoded.RecorderID)
	require.Equal(t, orig.PlateNo, decoded.PlateNo)
	require.Equal(t, orig.Salt, decoded.Salt)

	// Response variant (no salt).
	orig.Salt = nil
	encoded, err = orig.Encode()
	require.NoError(t, err)
	require.Len(t, encoded, 97)
	decoded = &RecorderParamSetting{}
	require.NoError(t, decoded.Decode(encoded))
	require.Nil(t, decoded.Salt)
}

func TestRecorderPositionTestRoundTrip(t *testing.T) {
	orig := &RecorderPositionTestRequest{Duration: 600}
	encoded, err := orig.Encode()
	require.NoError(t, err)
	require.Len(t, encoded, 2)

	decoded := &RecorderPositionTestRequest{}
	require.NoError(t, decoded.Decode(encoded))
	require.Equal(t, orig.Duration, decoded.Duration)

	// Stop command (empty block).
	stop := &RecorderPositionTestRequest{}
	encoded, err = stop.Encode()
	require.NoError(t, err)
	require.Empty(t, encoded)
	require.NoError(t, stop.Decode(nil))
	require.Equal(t, uint16(0), stop.Duration)
}

func TestRecorderPositionTestResponseRoundTrip(t *testing.T) {
	orig := &RecorderPositionTestResponse{
		Time:    "261029120000",
		GGAData: "$GPGGA,120000,3954.3265,N,11623.5432,E,1,08,1.0,45.3,M,,,,*47",
		RMCData: "$GPRMC,120000,A,3954.3265,N,11623.5432,E,0.0,0.0,291026,,,A*5C",
	}
	encoded, err := orig.Encode()
	require.NoError(t, err)

	decoded := &RecorderPositionTestResponse{}
	require.NoError(t, decoded.Decode(encoded))
	require.Equal(t, orig.Time, decoded.Time)
	require.Equal(t, orig.GGAData, decoded.GGAData)
	require.Equal(t, orig.RMCData, decoded.RMCData)
}

func TestRecorderDigestTestRoundTrip(t *testing.T) {
	req := &RecorderDigestTestRequest{CommTime: "261029120000"}
	encoded, err := req.Encode()
	require.NoError(t, err)
	require.Len(t, encoded, 6)

	decoded := &RecorderDigestTestRequest{}
	require.NoError(t, decoded.Decode(encoded))
	require.Equal(t, req.CommTime, decoded.CommTime)

	resp := &RecorderDigestTestResponse{
		RecorderTime: "261029120001",
		RecorderID:   make([]byte, 35),
		Digest:       make([]byte, 32),
	}
	for i := range resp.Digest {
		resp.Digest[i] = byte(i)
	}
	encoded, err = resp.Encode()
	require.NoError(t, err)
	require.Len(t, encoded, 73)

	decodedResp := &RecorderDigestTestResponse{}
	require.NoError(t, decodedResp.Decode(encoded))
	require.Equal(t, resp.RecorderTime, decodedResp.RecorderTime)
	require.Equal(t, resp.Digest, decodedResp.Digest)
}

func TestMsgRecordCommandTypedDecode(t *testing.T) {
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
