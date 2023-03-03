package datagen

import (
	"testing"
)

func Test_matchToken(t *testing.T) {
	type args struct {
		input string
	}
	tests := []struct {
		name      string
		args      args
		execTimes int
		repeated  bool
	}{
		{
			name: "case1: gen rand num",
			args: args{
				input: "[0-9]{8}",
			},
			execTimes: 10,
		},
		{
			name: "case2: gen rand num and str",
			args: args{
				input: "[0-9a-z]{3,6}",
			},
			execTimes: 10,
		},
		{
			name: "case3: no rand",
			args: args{
				input: "abcd",
			},
			execTimes: 10,
			repeated:  true,
		},
		{
			name: "case4: nums less than uint32",
			args: args{
				input: "[1-9][0-9]{0,8}|[1-3][0-9]{0,9}|4[0-2][0-9]{0,8}|43[0-1][0-9]{0,7}|32[0-9]{0,8}|31[0-9]{0,8}|30[0-9]{0,7}|2[0-9]{0,8}|1[0-9]{0,8}|0}",
			},
			execTimes: 1,
		},
		{
			name: "case5: nums between -90000000 and 90000000",
			args: args{
				input: "-?[1-8][0-9]{0,6}|-?9000000",
			},
			execTimes: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ansCollect := make(map[string]struct{})
			for i := 0; i < tt.execTimes; i++ {
				got := matchTokenAndGen(tt.args.input)
				t.Logf("gen rand result, got=%s", got)
				if _, ok := ansCollect[got]; ok && !tt.repeated {
					t.Errorf("gen rand result got repeated")
					t.FailNow()
				} else {
					ansCollect[got] = struct{}{}
				}
			}
		})
	}
}
