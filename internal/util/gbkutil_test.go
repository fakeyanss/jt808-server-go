package util

import (
	"reflect"
	"testing"
)

func TestUtf8ToGbk(t *testing.T) {
	type args struct {
		s []byte
	}
	tests := []struct {
		name    string
		args    args
		want    []byte
		wantErr bool
	}{
		{
			name:    "case1",
			args:    args{s: []byte{230, 181, 156, 239, 191, 189}},
			want:    []byte("京"),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Utf8ToGbk(tt.args.s)
			if (err != nil) != tt.wantErr {
				t.Errorf("Utf8ToGbk() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Utf8ToGbk() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGbkToUtf8(t *testing.T) {
	type args struct {
		s []byte
	}
	tests := []struct {
		name    string
		args    args
		want    []byte
		wantErr bool
	}{
		{
			name:    "case1",
			args:    args{s: []byte("京")},
			want:    []byte{230, 181, 156, 239, 191, 189},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GbkToUtf8(tt.args.s)
			if (err != nil) != tt.wantErr {
				t.Errorf("GbkToUtf8() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GbkToUtf8() = %v, want %v", got, tt.want)
			}
		})
	}
}
