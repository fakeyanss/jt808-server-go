package util

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBCD2NumberStr(t *testing.T) {
	type args struct {
		bcd []byte
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "case1: long number",
			args: args{bcd: []byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12}},
			want: "123456789012",
		},
		{
			name: "case1: short number",
			args: args{bcd: []byte{0x12, 0x34}},
			want: "1234",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BCD2NumberStr(tt.args.bcd)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNumberStr2BCD(t *testing.T) {
	type args struct {
		number string
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		{
			name: "case1: long number",
			args: args{number: "123456789012"},
			want: []byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12},
		},
		{
			name: "case2: short number",
			args: args{number: "1234"},
			want: []byte{0xff, 0xff, 0x12, 0x34},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NumberStr2BCD(tt.args.number)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestHex2Byte(t *testing.T) {
	type args struct {
		str string
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		{
			name: "case1: short str",
			args: args{str: "123456789012"},
			want: []byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12},
		},
		{
			name: "case2: long str",
			args: args{str: "000000000002080301CD779E0728C032003C0000008F230125145158"},
			want: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x08, 0x03, 0x01, 0xCD, 0x77, 0x9E, 0x07, 0x28, 0xC0, 0x32, 0x00, 0x3C, 0x00, 0x00, 0x00, 0x8F,
				0x23, 0x01, 0x25, 0x14, 0x51, 0x58},
		},
		{
			name: "case3: str of odd length",
			args: args{str: "12345678901"},
			want: []byte{0x12, 0x34, 0x56, 0x78, 0x90},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Hex2Byte(tt.args.str)
			require.Equal(t, tt.want, got)
		})
	}
}
