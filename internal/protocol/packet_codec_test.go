package protocol

import (
	stdhex "encoding/hex"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

func TestJT808PacketCodecStandardGoldenVectors(t *testing.T) {
	codec := NewJT808PacketCodec()
	cases := []struct {
		name string
		msg  model.JT808Msg
		want string
	}{
		{
			name: "2013 terminal heartbeat, empty body",
			msg: &model.Msg0002{Header: &model.MsgHeader{
				MsgID:        0x0002,
				Attr:         &model.MsgBodyAttr{VersionDesc: model.Version2013},
				PhoneNumber:  "013812345678",
				SerialNumber: 1,
			}},
			want: "7e000200000138123456780001327e",
		},
		{
			name: "2019 terminal heartbeat, versioned header",
			msg: &model.Msg0002{Header: &model.MsgHeader{
				MsgID:           0x0002,
				Attr:            &model.MsgBodyAttr{VersionDesc: model.Version2019, VersionSign: 1},
				ProtocolVersion: 1,
				PhoneNumber:     "01381234567890123456",
				SerialNumber:    1,
			}},
			want: "7e0002400001013812345678901234560001937e",
		},
		{
			name: "2013 terminal general acknowledgment",
			msg: &model.Msg0001{
				Header: &model.MsgHeader{
					MsgID:        0x0001,
					Attr:         &model.MsgBodyAttr{VersionDesc: model.Version2013},
					PhoneNumber:  "013812345678",
					SerialNumber: 2,
				},
				AnswerSerialNumber: 2,
				AnswerMessageID:    2,
				Result:             0,
			},
			want: "7e0001000501381234567800020002000200377e",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := codec.Encode(tc.msg)
			require.NoError(t, err)
			want, err := stdhex.DecodeString(tc.want)
			require.NoError(t, err)
			require.Equal(t, want, got)

			decoded, err := codec.Decode(want)
			require.NoError(t, err)
			require.Equal(t, tc.msg.GetHeader().MsgID, decoded.Header.MsgID)
			require.Equal(t, tc.msg.GetHeader().PhoneNumber, decoded.Header.PhoneNumber)
			require.Equal(t, tc.msg.GetHeader().SerialNumber, decoded.Header.SerialNumber)
			require.Equal(t, tc.msg.GetHeader().Attr.VersionDesc, decoded.Header.Attr.VersionDesc)
			switch msg := tc.msg.(type) {
			case *model.Msg0001:
				decodedMsg := &model.Msg0001{}
				require.NoError(t, decodedMsg.Decode(decoded))
				require.Equal(t, msg.AnswerSerialNumber, decodedMsg.AnswerSerialNumber)
				require.Equal(t, msg.AnswerMessageID, decodedMsg.AnswerMessageID)
				require.Equal(t, msg.Result, decodedMsg.Result)
			case *model.Msg0002:
				decodedMsg := &model.Msg0002{}
				require.NoError(t, decodedMsg.Decode(decoded))
			}
		})
	}
}

func TestJT808PacketCodecStandardFragmentEscapeVector(t *testing.T) {
	codec := NewJT808PacketCodec()
	header := &model.MsgHeader{
		MsgID: 0x0200,
		Attr: &model.MsgBodyAttr{
			VersionDesc:          model.Version2013,
			PacketFragmented:     1,
			PacketFragmentedDesc: model.PacketFragmentedTrue,
		},
		PhoneNumber:  "013812345678",
		SerialNumber: 1,
		Frag:         &model.MsgFragmentation{Total: 2, Index: 1},
	}
	want, err := stdhex.DecodeString("7e020020020138123456780001000200017d027d01107e")
	require.NoError(t, err)

	got, err := codec.EncodePacketData(header, []byte{0x7e, 0x7d})
	require.NoError(t, err)
	require.Equal(t, want, got)

	decoded, err := codec.Decode(want)
	require.NoError(t, err)
	require.False(t, decoded.SegCompleted)
	require.Equal(t, uint16(2), decoded.Header.Frag.Total)
	require.Equal(t, uint16(1), decoded.Header.Frag.Index)
	require.Equal(t, []byte{0x7e, 0x7d}, decoded.Body)
}

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
