package model

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeYanss/jt808-server-go/internal/codec/hex"
)

func TestMsg8100_Encode(t *testing.T) {
	type fields struct {
		Header             *MsgHeader
		AnswerSerialNumber uint16
		Result             ResultCodeType
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
				Header:             genMsgHeader(0x8100),
				AnswerSerialNumber: 0,
				Result:             0,
				AuthCode:           "test",
			},
			wantPkt: hex.Str2Byte("810040070112345678901234567890000100000074657374"),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Msg8100{
				Header:             tt.fields.Header,
				AnswerSerialNumber: tt.fields.AnswerSerialNumber,
				Result:             tt.fields.Result,
				AuthCode:           tt.fields.AuthCode,
			}
			gotPkt, err := m.Encode()
			require.Equal(t, tt.wantErr, err != nil, err)
			require.Equal(t, tt.wantPkt, gotPkt)
		})
	}
}
