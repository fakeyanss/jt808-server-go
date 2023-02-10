package model

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeYanss/jt808-server-go/internal/codec/hex"
)

func genMsgHeader() *MsgHeader {
	return &MsgHeader{
		MsgID: 0x8100,
		Attr: &MsgBodyAttr{
			Encryption:       0b000,
			PacketFragmented: 0,
			VersionSign:      1,
			Extra:            0,
		},
		ProtocolVersion: 1,
		PhoneNumber:     "1234567890",
		SerialNumber:    1,
		Frag:            nil,
	}
}

func TestCmd8100_Encode(t *testing.T) {
	type fields struct {
		Header             *MsgHeader
		AnswerSerialNumber uint16
		Result             CmdResult
		AuthCode           string
	}
	tests := []struct {
		name    string
		fields  fields
		wantPkt []byte
		wantErr bool
	}{
		{
			name: "case1: 0x8100 encode",
			fields: fields{
				Header:             genMsgHeader(),
				AnswerSerialNumber: 0,
				Result:             0,
				AuthCode:           "test",
			},
			wantPkt: hex.Str2Byte("81004007011234567890000100000074657374"),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Cmd8100{
				Header:             tt.fields.Header,
				AnswerSerialNumber: tt.fields.AnswerSerialNumber,
				Result:             tt.fields.Result,
				AuthCode:           tt.fields.AuthCode,
			}
			gotPkt, err := c.Encode()
			require.Equal(t, tt.wantErr, err != nil, err)
			require.Equal(t, tt.wantPkt, gotPkt)
		})
	}
}
