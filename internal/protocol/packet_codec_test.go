package protocol

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
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
				VerifyCode: 251,
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

func TestJT808PacketCodecRejectsMalformedFrames(t *testing.T) {
	pc := &JT808PacketCodec{}
	tests := [][]byte{
		hex.Str2Byte("7e"),
		hex.Str2Byte("7e7e"),
		hex.Str2Byte("7e0200001c2234567890150000000000000002080301CD779E0728C032003C0000008F230125145158FB7F"),
		append(append([]byte{0x7e}, make([]byte, 12)...), 0x7d, 0x03, 0x7e),
	}
	for _, frame := range tests {
		_, err := pc.Decode(frame)
		require.Error(t, err)
	}
}

func TestJT808PacketCodecReassemblesOutOfOrderFragments(t *testing.T) {
	codec := &JT808PacketCodec{}
	phone := "188765432199"
	parts := [][]byte{[]byte("ABCD"), []byte("EFGH"), []byte("IJ")}
	order := []int{2, 0, 1}
	var completed *model.PacketData
	for _, partIndex := range order {
		header := &model.MsgHeader{
			MsgID:        0x0102,
			Attr:         &model.MsgBodyAttr{VersionDesc: model.Version2013, PacketFragmented: 1, PacketFragmentedDesc: model.PacketFragmentedTrue},
			PhoneNumber:  phone,
			SerialNumber: uint16(50 + partIndex),
			Frag:         &model.MsgFragmentation{Total: 3, Index: uint16(partIndex + 1)},
		}
		frame, err := codec.EncodePacketData(header, parts[partIndex])
		require.NoError(t, err)
		packet, err := codec.Decode(frame)
		require.NoError(t, err)
		if packet.SegCompleted {
			completed = packet
		}
	}
	require.NotNil(t, completed)
	require.Equal(t, []byte("ABCDEFGHIJ"), completed.Body)
	require.Equal(t, uint16(50), completed.Header.SerialNumber)
	require.False(t, completed.Header.IsFragmented())
	require.Nil(t, completed.Header.Frag)
}
