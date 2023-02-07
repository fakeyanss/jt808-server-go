package model

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeYanss/jt808-server-go/internal/util"
)

func genMsgHeader() *MsgHeader {
	return &MsgHeader{
		MsgID: 0x8100,
		MsgBodyAttr: MsgBodyAttr{
			Encryption:       EncryptionNone,
			PacketFragmented: false,
			VersionSign:      false,
			// 加密方式原文
		},
		ProtocolVersion:  1,
		PhoneNumber:      "1234567890",
		SerialNumber:     1,
		MsgFragmentation: MsgFragmentation{},
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
			wantPkt: util.Hex2Byte("8100001e0123456789017fff970c00636877443053453166636877443053453166636877443053453166"),
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
