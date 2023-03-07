package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

func TestMsg8004_Encode(t *testing.T) {
	testTime, _ := time.Parse("060102150405", "230301123456")
	type fields struct {
		Header     *MsgHeader
		ServerTime *time.Time
	}
	tests := []struct {
		name    string
		fields  fields
		wantPkt []byte
		wantErr bool
	}{
		{
			name: "case1: test time format",
			fields: fields{
				Header:     genMsgHeader(MsgID8004),
				ServerTime: &testTime,
			},
			wantPkt: hex.Str2Byte("8004400601123456789012345678900001230301123456"),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Msg8004{
				Header:     tt.fields.Header,
				ServerTime: tt.fields.ServerTime,
			}
			gotPkt, err := m.Encode()
			assert.Equal(t, tt.wantErr, err != nil, err)
			assert.Equal(t, tt.wantPkt, gotPkt)
		})
	}
}
