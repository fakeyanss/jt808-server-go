package model

import (
	"testing"
)

func TestMsg0001_Decode(t *testing.T) {
	type fields struct {
		Header             *MsgHeader
		AnswerSerialNumber uint16
		AnswerMessageID    uint16
		Result             uint8
	}
	type args struct {
		pkt *Packet
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Msg0001{
				Header:             tt.fields.Header,
				AnswerSerialNumber: tt.fields.AnswerSerialNumber,
				AnswerMessageID:    tt.fields.AnswerMessageID,
				Result:             tt.fields.Result,
			}
			if err := m.Decode(tt.args.pkt); (err != nil) != tt.wantErr {
				t.Errorf("Msg0001.Decode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMsg0002_Decode(t *testing.T) {
	type fields struct {
		Header *MsgHeader
	}
	type args struct {
		pkt *Packet
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Msg0002{
				Header: tt.fields.Header,
			}
			if err := m.Decode(tt.args.pkt); (err != nil) != tt.wantErr {
				t.Errorf("Msg0002.Decode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
