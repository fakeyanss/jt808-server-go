package bcd

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
