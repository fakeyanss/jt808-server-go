package protocol

import (
	"encoding/json"
	"testing"

	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeYanss/jt808-server-go/internal/util"
)

func TestJT808PacketCodec_Decode(t *testing.T) {

	type args struct {
		payload []byte
	}
	tests := []struct {
		name    string
		pc      *JT808PacketCodec
		args    args
		want    *model.Packet
		wantErr bool
	}{
		{
			name: "case1",
			pc:   &JT808PacketCodec{},
			args: args{
				payload: util.Hex2Byte("7E0200001c2234567890150000000000000002080301cd48b50728d22b003902bc008f2301251438137d027E"),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "case2",
			pc:   &JT808PacketCodec{},
			args: args{
				payload: util.Hex2Byte("7E0200001C2234567890150000000000000002080301CD779E0728C032003C0000008F230125145158FB7E"),
			},
			want: &model.Packet{
				Header: &model.MsgHeader{
					MsgID: 0x0200,
					MsgBodyAttr: model.MsgBodyAttr{
						BodyLength:       28,
						Encryption:       model.EncryptionNone,
						PacketFragmented: false,
						VersionSign:      false,
						Extra:            0,
					},
					ProtocolVersion: 0,
					PhoneNumber:     "223456789015",
					SerialNumber:    0,
					MsgFragmentation: model.MsgFragmentation{
						Total: 0,
						Index: 0,
					},
				},
				Body:       util.Hex2Byte("000000000002080301CD779E0728C032003C0000008F230125145158"),
				VerifyCode: 126,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &JT808PacketCodec{}
			got, err := pc.Decode(tt.args.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("JT808PacketCodec.Decode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			gotJson, _ := json.Marshal(got)
			wantJson, _ := json.Marshal(tt.want)
			if string(gotJson) != string(wantJson) {
				t.Errorf("JT808PacketCodec.Decode() = %v, want %v", string(gotJson), string(wantJson))
			}
		})
	}
}
