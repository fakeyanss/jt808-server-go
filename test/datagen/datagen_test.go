package datagen

import (
	"testing"
)

func Test_matchTokenAndGen(t *testing.T) {
	type args struct {
		input string
	}
	tests := []struct {
		name        string
		args        args
		execTimes   int
		repeatNoErr bool
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
			execTimes:   10,
			repeatNoErr: true,
		},
		{
			name: "case4: 0 or 1",
			args: args{
				input: "0|1",
			},
			execTimes:   4,
			repeatNoErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ansCollect := make(map[string]struct{})
			for i := 0; i < tt.execTimes; i++ {
				got := matchTokenAndGen(tt.args.input)
				t.Logf("gen rand result, got=%s", got)
				if _, ok := ansCollect[got]; ok && !tt.repeatNoErr {
					t.Errorf("gen rand result got repeated")
					t.FailNow()
				} else {
					ansCollect[got] = struct{}{}
				}
			}
		})
	}
}
