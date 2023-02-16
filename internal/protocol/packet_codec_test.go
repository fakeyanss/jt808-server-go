package protocol

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeYanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
)

func TestJT808PacketCodec_Decode(t *testing.T) {
	type args struct {
		payload []byte
	}
	tests := []struct {
		name    string
		pc      *JT808PacketCodec
		args    args
		want    *model.PacketData
		wantErr bool
	}{
		{
			name: "case1",
			pc:   &JT808PacketCodec{},
			args: args{
				payload: hex.Str2Byte("7E0200001c2234567890150000000000000002080301cd48b50728d22b003902bc008f2301251438137d027E"),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "case2",
			pc:   &JT808PacketCodec{},
			args: args{
				payload: hex.Str2Byte("7E0200001C2234567890150000000000000002080301CD779E0728C032003C0000008F230125145158FB7E"),
			},
			want: &model.PacketData{
				Header: &model.MsgHeader{
					MsgID: 0x0200,
					Attr: &model.MsgBodyAttr{
						BodyLength:       28,
						Encryption:       0b000,
						PacketFragmented: 0,
						VersionSign:      0,
						Extra:            0,

						VersionDesc: model.Version2013,
					},
					ProtocolVersion: 0,
					PhoneNumber:     "223456789015",
					SerialNumber:    0,
					Frag:            nil,
				},
				Body:       hex.Str2Byte("000000000002080301CD779E0728C032003C0000008F230125145158"),
				VerifyCode: 126,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &JT808PacketCodec{}
			got, err := pc.Decode(tt.args.payload)
			require.Equal(t, tt.wantErr, err != nil, err)
			if tt.want == nil {
				return
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestJT808PacketCodec_genVerifier(t *testing.T) {
	type args struct {
		pkt []byte
	}
	tests := []struct {
		name string
		pc   *JT808PacketCodec
		args args
		want []byte
	}{
		{
			name: "case1",
			pc:   &JT808PacketCodec{},
			args: args{pkt: hex.Str2Byte("000140050100000000017299841738ffff007b01c803")},
			want: append(hex.Str2Byte("000140050100000000017299841738ffff007b01c803"), 0xb5),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &JT808PacketCodec{}
			if got := pc.genVerifier(tt.args.pkt); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("JT808PacketCodec.genVerifier() = %v, want %v", got, tt.want)
			}
		})
	}
}
