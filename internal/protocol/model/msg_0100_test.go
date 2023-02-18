package model

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

func TestMsg0100_Encode(t *testing.T) {
	type fields struct {
		Header         *MsgHeader
		ProvinceID     uint16
		CityID         uint16
		ManufacturerID string
		DeviceMode     string
		DeviceID       string
		PlateColor     byte
		PlateNumber    string
	}
	tests := []struct {
		name    string
		fields  fields
		wantPkt []byte
		wantErr bool
	}{
		{
			name: "case1: 0x0100 encode",
			fields: fields{
				Header:         genMsgHeader(0x0100),
				ProvinceID:     31,
				CityID:         115,
				ManufacturerID: "fakeyanss",
				DeviceMode:     "fakeyanss.github.io",
				DeviceID:       "1234ABCD",
				PlateColor:     1,
				PlateNumber:    "京A12345",
			},
			wantPkt: hex.Str2Byte("0100405401123456789012345678900001001f007366616b6579616e" +
				"7373000066616b6579616e73732e6769746875622e696f0000000000000000000000313233" +
				"34414243440000000000000000000000000000000000000000000001bea9413132333435"),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Msg0100{
				Header:         tt.fields.Header,
				ProvinceID:     tt.fields.ProvinceID,
				CityID:         tt.fields.CityID,
				ManufacturerID: tt.fields.ManufacturerID,
				DeviceMode:     tt.fields.DeviceMode,
				DeviceID:       tt.fields.DeviceID,
				PlateColor:     tt.fields.PlateColor,
				PlateNumber:    tt.fields.PlateNumber,
			}
			gotPkt, err := m.Encode()
			require.Equal(t, tt.wantErr, err != nil, err)
			require.Equal(t, tt.wantPkt, gotPkt)
		})
	}
}
