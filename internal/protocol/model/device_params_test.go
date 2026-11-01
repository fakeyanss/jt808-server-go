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
