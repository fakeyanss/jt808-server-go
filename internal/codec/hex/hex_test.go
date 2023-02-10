package hex

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStr2Byte(t *testing.T) {
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
			got := Str2Byte(tt.args.str)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestByte2Str(t *testing.T) {
	type args struct {
		src []byte
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "case1: byte to str",
			args: args{src: []byte{0x12, 0x34, 0x56, 0x78}},
			want: "12345678",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Byte2Str(tt.args.src)
			require.Equal(t, tt.want, got)
		})
	}
}
