package model

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

func TestMsgHeader_Decode(t *testing.T) {
	argMap := make(map[string][]byte, 0)
	argMap["case1"] = hex.Str2Byte("010000212234567890150000")

	type args struct {
		pkt []byte
	}
	tests := []struct {
		name    string
		args    args
		want    MsgHeader
		wantErr bool
	}{
		{
			name:    "case1",
			args:    args{pkt: hex.Str2Byte("0100400001123456789012345678900001")},
			want:    *genMsgHeader(0x0100),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &MsgHeader{}
			err := h.Decode(tt.args.pkt)
			h.Idx = 0 // reset idx
			require.Equal(t, tt.wantErr, err != nil, err)
			require.Equal(t, tt.want, *h)
		})
	}
}

func TestMsgHeader_Encode(t *testing.T) {
	tests := []struct {
		name    string
		want    []byte
		wantErr bool
	}{
		{
			name:    "case1: header encode",
			want:    hex.Str2Byte("0100400001123456789012345678900001"),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := genMsgHeader(0x0100)
			got, err := h.Encode()
			require.Equal(t, tt.wantErr, err != nil, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMsgBodyAttr_Decode(t *testing.T) {
	type fields struct {
		BodyLength           uint16
		Encryption           uint8
		PacketFragmented     uint8
		VersionSign          uint8
		Extra                uint8
		EncryptionDesc       EncryptionType
		PacketFragmentedDesc PacketFragmentedType
		VersionDesc          VersionType
	}
	type args struct {
		bitNum uint16
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
			attr := &MsgBodyAttr{
				BodyLength:           tt.fields.BodyLength,
				Encryption:           tt.fields.Encryption,
				PacketFragmented:     tt.fields.PacketFragmented,
				VersionSign:          tt.fields.VersionSign,
				Extra:                tt.fields.Extra,
				EncryptionDesc:       tt.fields.EncryptionDesc,
				PacketFragmentedDesc: tt.fields.PacketFragmentedDesc,
				VersionDesc:          tt.fields.VersionDesc,
			}
			if err := attr.Decode(tt.args.bitNum); (err != nil) != tt.wantErr {
				t.Errorf("MsgBodyAttr.Decode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMsgBodyAttr_Encode(t *testing.T) {
	type fields struct {
		BodyLength           uint16
		Encryption           uint8
		PacketFragmented     uint8
		VersionSign          uint8
		Extra                uint8
		EncryptionDesc       EncryptionType
		PacketFragmentedDesc PacketFragmentedType
		VersionDesc          VersionType
	}
	tests := []struct {
		name   string
		fields fields
		want   uint16
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr := &MsgBodyAttr{
				BodyLength:           tt.fields.BodyLength,
				Encryption:           tt.fields.Encryption,
				PacketFragmented:     tt.fields.PacketFragmented,
				VersionSign:          tt.fields.VersionSign,
				Extra:                tt.fields.Extra,
				EncryptionDesc:       tt.fields.EncryptionDesc,
				PacketFragmentedDesc: tt.fields.PacketFragmentedDesc,
				VersionDesc:          tt.fields.VersionDesc,
			}
			if got := attr.Encode(); got != tt.want {
				t.Errorf("MsgBodyAttr.Encode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMsgFragmentation_Decode(t *testing.T) {
	type fields struct {
		Total uint16
		Index uint16
	}
	type args struct {
		pkt []byte
		idx *int
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
			frag := &MsgFragmentation{
				Total: tt.fields.Total,
				Index: tt.fields.Index,
			}
			if err := frag.Decode(tt.args.pkt, tt.args.idx); (err != nil) != tt.wantErr {
				t.Errorf("MsgFragmentation.Decode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMsgFragmentation_Encode(t *testing.T) {
	type fields struct {
		Total uint16
		Index uint16
	}
	tests := []struct {
		name   string
		fields fields
		want   []byte
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frag := &MsgFragmentation{
				Total: tt.fields.Total,
				Index: tt.fields.Index,
			}
			if got := frag.Encode(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MsgFragmentation.Encode() = %v, want %v", got, tt.want)
			}
		})
	}
}
