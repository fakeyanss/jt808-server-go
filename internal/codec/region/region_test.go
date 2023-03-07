package region

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParse(t *testing.T) {
	type args struct {
		code string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "case1: 130400",
			args: args{code: "130400"},
			want: "邯郸市",
		},
		{
			name: "case2: 610322",
			args: args{code: "610322"},
			want: "凤翔县",
		},
		{
			name: "case3: 999322",
			args: args{code: "999322"},
			want: CodeNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.args.code)
			assert.Equal(t, tt.want, got.Name)
		})
	}
}
