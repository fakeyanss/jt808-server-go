package protocol

import (
	"io"
	"reflect"
	"testing"
)

func TestJT808FrameCodec_Decode(t *testing.T) {
	type args struct {
		r io.Reader
	}
	tests := []struct {
		name    string
		pc      *JT808FrameCodec
		args    args
		want    FramePayload
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &JT808FrameCodec{}
			got, err := pc.Read(tt.args.r)
			if (err != nil) != tt.wantErr {
				t.Errorf("JT808FrameCodec.Decode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("JT808FrameCodec.Decode() = %v, want %v", got, tt.want)
			}
		})
	}
}
