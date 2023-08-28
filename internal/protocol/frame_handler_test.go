package protocol

import (
	"bufio"
	"bytes"
	"context"
	"testing"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/stretchr/testify/require"
)

func TestJT808Frame_handler_recv(t *testing.T) {
	type args struct {
		payload []byte
	}
	tests := []struct {
		name    string
		args    args
		want    [][]byte
		wantErr bool
	}{
		{
			name: "2 packages, garbage at middle",
			args: args{
				payload: hex.Str2Byte("7e5622664455e2447e549955777e1122334455668899557e"),
			},
			want: [][]byte{
				hex.Str2Byte("7e5622664455e2447e"),
				hex.Str2Byte("7e1122334455668899557e"),
			},
			wantErr: false,
		},
		{
			name: "1 package, garbage at middle and last section",
			args: args{
				payload: hex.Str2Byte("7e5622664455e2447e549955777e112233445566889955"),
			},
			want: [][]byte{
				hex.Str2Byte("7e5622664455e2447e"),
			},
			wantErr: false,
		},
		{
			name: "2 packages, garbage at first and last section",
			args: args{
				payload: hex.Str2Byte("55667e5622664455e2447e7e1122334455668899557e44"),
			},
			want: [][]byte{
				hex.Str2Byte("7e5622664455e2447e"),
				hex.Str2Byte("7e1122334455668899557e"),
			},
			wantErr: false,
		},
		{
			name: "2 packages, garbage at first section",
			args: args{
				payload: hex.Str2Byte("55667e5622664455e2447e7e1122334455668899557e"),
			},
			want: [][]byte{
				hex.Str2Byte("7e5622664455e2447e"),
				hex.Str2Byte("7e1122334455668899557e"),
			},
			wantErr: false,
		},
		{
			name: "2 packages, garbage at last section",
			args: args{
				payload: hex.Str2Byte("7e5622664455e2447e7e1122334455668899557e44"),
			},
			want: [][]byte{
				hex.Str2Byte("7e5622664455e2447e"),
				hex.Str2Byte("7e1122334455668899557e"),
			},
			wantErr: false,
		},
		{
			name: "2 packages",
			args: args{
				payload: hex.Str2Byte("7e5622664455e2447e7e1122334455668899557e"),
			},
			want: [][]byte{
				hex.Str2Byte("7e5622664455e2447e"),
				hex.Str2Byte("7e1122334455668899557e"),
			},
			wantErr: false,
		},
		{
			name: "1 package, garbage at first and last section",
			args: args{
				payload: hex.Str2Byte("557e1122334455668899557e44"),
			},
			want: [][]byte{
				hex.Str2Byte("7e1122334455668899557e"),
			},
			wantErr: false,
		},
		{
			name: "1 package, garbage at first section",
			args: args{
				payload: hex.Str2Byte("55447e1122334455668899557e"),
			},
			want: [][]byte{
				hex.Str2Byte("7e1122334455668899557e"),
			},
			wantErr: false,
		},
		{
			name: "1 package, garbage at last section",
			args: args{
				payload: hex.Str2Byte("7e1122334455668899557e44"),
			},
			want: [][]byte{
				hex.Str2Byte("7e1122334455668899557e"),
			},
			wantErr: false,
		},
		{
			name: "1 package",
			args: args{
				payload: hex.Str2Byte("7e1122334455668899557e"),
			},
			want: [][]byte{
				hex.Str2Byte("7e1122334455668899557e"),
			},
			wantErr: false,
		},
		{
			name: "0 package",
			args: args{
				payload: hex.Str2Byte("1122334455668899557e"),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bytes.NewReader(tt.args.payload)
			fh := &JT808FrameHandler{
				rbuf: bufio.NewReader(reader),
			}
			for _, v := range tt.want {
				fr, err := fh.Recv(context.Background())
				require.Equal(t, tt.wantErr, err != nil, err)
				require.Equal(t, FramePayload(v), fr)
			}
		})
	}
}
