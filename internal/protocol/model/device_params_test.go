package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeviceArgs_Decode(t *testing.T) {
	type fields struct {
		ArgCnt uint8
		Args   []*ParamData
	}
	type args struct {
		cnt uint8
		pkt []byte
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "case1: decode args",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := &DeviceParams{
				ParamCnt: tt.fields.ArgCnt,
				Params:   tt.fields.Args,
			}
			got := &DeviceParams{}
			err := got.Decode("", tt.args.cnt, tt.args.pkt)
			require.Equal(t, tt.wantErr, err != nil, err)
			require.Equal(t, want, got)
		})
	}
}
