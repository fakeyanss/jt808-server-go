package util

import (
	"reflect"
	"testing"
)

func TestBcd2NumberStr(t *testing.T) {
	type args struct {
		bcd []byte
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "case1",
			args: args{bcd: []byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12}},
			want: "123456789012",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Bcd2NumberStr(tt.args.bcd); got != tt.want {
				t.Errorf("Bcd2NumberStr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNumberStr2bcd(t *testing.T) {
	type args struct {
		number string
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		{
			name: "case1",
			args: args{number: "123456789012"},
			want: []byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NumberStr2bcd(tt.args.number); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NumberStr2bcd() = %v, want %v", got, tt.want)
			}
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
			name: "case1",
			args: args{str: "123456789012"},
			want: []byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12},
		},
		{
			name: "case2",
			args: args{str: "000000000002080301CD779E0728C032003C0000008F230125145158"},
			want: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x08, 0x03, 0x01, 0xCD, 0x77, 0x9E, 0x07, 0x28, 0xC0, 0x32, 0x00, 0x3C, 0x00, 0x00, 0x00, 0x8F, 0x23, 0x01, 0x25, 0x14, 0x51, 0x58},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Hex2Byte(tt.args.str); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Hex2Byte() = %v, want %v", got, tt.want)
			}
		})
	}
}
