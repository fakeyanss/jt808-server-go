package model

import (
	"testing"

	"github.com/fakeYanss/jt808-server-go/internal/util"
)

func TestJT808MsgHeader_Decode(t *testing.T) {
	argMap := make(map[string][]byte, 0)
	argMap["case1"] = util.Hex2Byte("010000212234567890150000")

	type fields struct {
		MsgID uint16
		JT808MsgBodyAttr
		ProtocolVersion byte
		PhoneNumber     string
		SerialNumber    uint16
		JT808MsgFragmentation
	}
	type args struct {
		pkt []byte
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name:    "case1",
			fields:  fields{},
			args:    args{pkt: argMap["case1"]},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &JT808MsgHeader{
				MsgID:           tt.fields.MsgID,
				ProtocolVersion: tt.fields.ProtocolVersion,
				PhoneNumber:     tt.fields.PhoneNumber,
				SerialNumber:    tt.fields.SerialNumber,
			}
			if err := h.Decode(tt.args.pkt); (err != nil) != tt.wantErr {
				t.Errorf("JT808MsgHeader.Decode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
