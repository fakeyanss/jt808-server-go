package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_location_Decode(t *testing.T) {
	type args struct {
		m *Msg0200
	}
	tests := []struct {
		name string
		args args
		want Location
	}{
		{
			name: "case1: test latitude and longitude accuracy",
			args: args{
				m: &Msg0200{
					Latitude:  116307629,
					Longitude: 40058359,
					Altitude:  312,
				},
			},
			want: Location{
				Latitude:  116.307629,
				Longitude: 40.058359,
				Altitude:  312,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &Location{}
			l.Decode(tt.args.m)
			assert.Equal(t, tt.want, *l)
		})
	}
}

func Test_drive_Decode(t *testing.T) {
	type fields struct {
		Speed     float64
		Direction uint16
	}
	type args struct {
		m *Msg0200
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		{
			name: "case1: test drive speed accuracy",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Drive{
				Speed:     tt.fields.Speed,
				Direction: tt.fields.Direction,
			}
			d.Decode(tt.args.m)
		})
	}
}
