package hash

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFNV32(t *testing.T) {
	type args struct {
		src string
	}
	tests := []struct {
		name string
		args args
		want uint32
	}{
		{
			name: "case1: hash string",
			args: args{src: "abcDEF"},
			want: uint32(1099054474),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FNV32(tt.args.src)
			require.Equal(t, tt.want, got)
		})
	}
}
