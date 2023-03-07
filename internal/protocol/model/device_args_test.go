package model

import "testing"

func TestDeviceArgs_Decode(t *testing.T) {
	type fields struct {
		ArgCnt uint8
		Args   []*ArgData
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
			a := &DeviceArgs{
				ArgCnt: tt.fields.ArgCnt,
				Args:   tt.fields.Args,
			}
			if err := a.Decode(tt.args.cnt, tt.args.pkt); (err != nil) != tt.wantErr {
				t.Errorf("DeviceArgs.Decode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
