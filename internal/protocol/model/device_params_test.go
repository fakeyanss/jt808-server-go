package model

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

func encodeTestParam(id uint32, value []byte) []byte {
	pkt := hex.WriteDoubleWord(nil, id)
	pkt = hex.WriteByte(pkt, uint8(len(value)))
	return hex.WriteBytes(pkt, value)
}

func TestSubiaoParamsRoundTrip(t *testing.T) {
	adas := SubiaoADASParam{
		AlarmSpeedThreshold:              30,
		AlarmVolume:                      6,
		ActivePhotoStrategy:              1,
		ActiveTimedPhotoInterval:         60,
		ActiveDistancePhotoInterval:      200,
		SingleActivePhotoCount:           3,
		SingleActivePhotoInterval:        2,
		PhotoResolution:                  1,
		VideoResolution:                  1,
		AlarmEnable:                      0x00010FFF,
		EventEnable:                      3,
		ObstacleAlarmDistanceThreshold:   30,
		ObstacleAlarmSpeedThreshold:      50,
		FrequentLaneChangeDetectInterval: 60,
		FrequentLaneChangeDetectCount:    5,
		RoadSignRecognitionPhotoCount:    3,
		RoadSignRecognitionPhotoInterval: 2,
		TailReserved:                     []byte{0, 0, 0, 0},
	}
	dsm := SubiaoDSMParam{
		AlarmSpeedThreshold:              30,
		AlarmVolume:                      6,
		AlarmEnable:                      0x000001FF,
		EventEnable:                      3,
		SmokingAlarmInterval:             180,
		PhoneCallAlarmInterval:           120,
		MiddleReserved:                   []byte{0, 0, 0},
		FatigueSpeedThreshold:            50,
		DriverIdentityRecognitionTrigger: 1,
		TailReserved:                     []byte{0, 0},
	}
	tpms := SubiaoTPMSParam{
		TireSpecification:        "195/65R15 91",
		TirePressureUnit:         3,
		NormalPressure:           140,
		ImbalanceThreshold:       20,
		SlowLeakThreshold:        5,
		LowPressureThreshold:     110,
		HighPressureThreshold:    189,
		HighTemperatureThreshold: 80,
		VoltageThreshold:         10,
		TimedReportInterval:      60,
		Reserved:                 []byte{0, 0, 0, 0, 0, 0},
	}
	bsd := SubiaoBSDParam{RearApproachAlarmTimeThreshold: 3, SideRearApproachAlarmTimeThreshold: 5}

	cases := []struct {
		id       uint32
		value    any
		wantLen  int
		expected any
	}{
		{id: 0xF364, value: adas, wantLen: 56, expected: adas},
		{id: 0xF365, value: dsm, wantLen: 49, expected: dsm},
		{id: 0xF366, value: tpms, wantLen: 36, expected: tpms},
		{id: 0xF367, value: bsd, wantLen: 2, expected: bsd},
	}
	for _, tt := range cases {
		payload, err := argTable[tt.id].encodeErr(tt.value)
		require.NoError(t, err)
		require.Len(t, payload, tt.wantLen, "param 0x%04x", tt.id)

		params := &DeviceParams{}
		require.NoError(t, params.Decode("", 1, encodeTestParam(tt.id, payload)))
		require.Equal(t, tt.expected, params.Params[0].ParamValue, "param 0x%04x", tt.id)

		// 解码结果再次编码应逐字节一致（DeviceParams.Encode 会先写参数个数）。
		reencoded, err := params.Encode()
		require.NoError(t, err)
		expected := hex.WriteByte(nil, 1)
		expected = hex.WriteBytes(expected, encodeTestParam(tt.id, payload))
		require.Equal(t, expected, reencoded)
	}
}

func TestSubiaoParamsEncodeFromJSONMap(t *testing.T) {
	// API 下发的参数值经 encoding/json 后是 map[string]any，数字为 float64。
	value := map[string]any{
		"alarmSpeedThreshold": float64(30),
		"alarmVolume":         float64(6),
		"alarmEnable":         float64(0x00010FFF),
		"tailReserved":        []any{0, 0, 0, 0},
	}
	payload, err := encodeSubiaoADASParam(value)
	require.NoError(t, err)
	require.Len(t, payload, 56)

	var decoded SubiaoADASParam
	decodedValue, err := decodeSubiaoADASParam(payload)
	require.NoError(t, err)
	decoded = decodedValue.(SubiaoADASParam)
	require.Equal(t, uint8(30), decoded.AlarmSpeedThreshold)
	require.Equal(t, uint8(6), decoded.AlarmVolume)
	require.Equal(t, uint32(0x00010FFF), decoded.AlarmEnable)
	require.Equal(t, []byte{0, 0, 0, 0}, decoded.TailReserved)
}

func TestSubiaoParamDecodeRejectsShortValue(t *testing.T) {
	params := &DeviceParams{}
	err := params.Decode("", 1, encodeTestParam(0xF365, make([]byte, 46)))
	require.Error(t, err)
}

func TestSubiaoParamsDecodeWithoutReserved(t *testing.T) {
	// 部分终端省略尾部保留字段，只要有效字段完整仍应解码成功。
	adas, err := decodeSubiaoADASParam(make([]byte, 52))
	require.NoError(t, err)
	require.Nil(t, adas.(SubiaoADASParam).TailReserved)
	_, err = decodeSubiaoTPMSParam(make([]byte, 30))
	require.NoError(t, err)

	bsd, err := decodeSubiaoBSDParam([]byte{1, 2})
	require.NoError(t, err)
	require.Equal(t, SubiaoBSDParam{RearApproachAlarmTimeThreshold: 1, SideRearApproachAlarmTimeThreshold: 2}, bsd)
}

func TestDeviceParamsEncodeRejectsUnknownParam(t *testing.T) {
	params := &DeviceParams{Params: []*ParamData{{ParamID: 0x00FF, ParamValue: []byte{1, 2, 3}}}}
	_, err := params.Encode()
	require.ErrorIs(t, err, ErrEncodeDeviceParams)
	require.ErrorIs(t, err, ErrParamIDNotSupportted)
}

func TestDeviceParamsEncodeRejectsInvalidCompoundValue(t *testing.T) {
	params := &DeviceParams{Params: []*ParamData{{
		ParamID:    0xF364,
		ParamValue: map[string]any{"alarmSpeedThreshold": float64(300)},
	}}}
	_, err := params.Encode()
	require.ErrorIs(t, err, ErrEncodeDeviceParams)
}

func TestMsg0104DecodesSubiaoParams(t *testing.T) {
	payload, err := encodeSubiaoBSDParam(SubiaoBSDParam{RearApproachAlarmTimeThreshold: 4, SideRearApproachAlarmTimeThreshold: 6})
	require.NoError(t, err)
	body := hex.WriteWord(nil, 7)
	body = hex.WriteByte(body, 1)
	body = hex.WriteBytes(body, encodeTestParam(0xF367, payload))

	msg := &Msg0104{}
	err = msg.Decode(&PacketData{Header: genMsgHeader(0x0104), Body: body})
	require.NoError(t, err)
	require.Equal(t, uint16(7), msg.AnswerSerialNumber)
	require.Len(t, msg.Parameters.Params, 1)
	require.Equal(t, SubiaoBSDParam{RearApproachAlarmTimeThreshold: 4, SideRearApproachAlarmTimeThreshold: 6}, msg.Parameters.Params[0].ParamValue)
}

func TestDeviceParamsDecode(t *testing.T) {
	tests := []struct {
		name    string
		pkt     []byte
		cnt     uint8
		want    []*ParamData
		wantErr bool
	}{
		{
			name: "known params",
			pkt: append(
				encodeTestParam(0x0001, []byte{0, 0, 0, 1}),
				encodeTestParam(0x0031, []byte{0, 100})...,
			),
			cnt: 2,
			want: []*ParamData{
				{ParamID: 0x0001, ParamLen: 4, ParamValue: uint32(1)},
				{ParamID: 0x0031, ParamLen: 2, ParamValue: uint16(100)},
			},
		},
		{
			name: "unknown param is skipped",
			pkt: append(
				encodeTestParam(0x00FF, []byte{1, 2, 3}),
				encodeTestParam(0x0001, []byte{0, 0, 0, 2})...,
			),
			cnt: 2,
			want: []*ParamData{
				{ParamID: 0x00FF, ParamLen: 3, ParamValue: []byte{1, 2, 3}},
				{ParamID: 0x0001, ParamLen: 4, ParamValue: uint32(2)},
			},
		},
		{
			name: "zero length string param",
			pkt:  encodeTestParam(0x0110, nil),
			cnt:  1,
			want: []*ParamData{{ParamID: 0x0110, ParamLen: 0, ParamValue: ""}},
		},
		{
			name:    "declared length exceeds body",
			pkt:     []byte{0, 0, 0, 1, 6, 0, 0, 0, 1},
			cnt:     1,
			wantErr: true,
		},
		{
			name:    "incomplete param header",
			pkt:     []byte{0, 0, 0},
			cnt:     1,
			wantErr: true,
		},
		{
			name:    "fixed width param is truncated",
			pkt:     encodeTestParam(0x0001, []byte{0, 1}),
			cnt:     1,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &DeviceParams{}
			err := got.Decode("", tt.cnt, tt.pkt)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got.Params)
		})
	}
}
