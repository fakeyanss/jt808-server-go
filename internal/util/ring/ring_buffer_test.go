// Package ring, Simple RingBuffer pacakge
// from https://github.com/sahmad98/go-ringbuffer/blob/master/ring_buffer.go
//
// Ringbuffer is non blocking for readers and writers, writers will
// overwrite older data in a circular fashion. Readers will read
// from the current position and update it.
package ring

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRingBuffer(t *testing.T) {
	type args struct {
		size int32
	}
	tests := []struct {
		name string
		args args
		want *RingBuffer
	}{
		{
			name: "case1: new ringbuffer",
			args: args{
				size: 5,
			},
			want: &RingBuffer{
				Size:      5,
				Container: make([]any, 5),
				Reader:    0,
				Writer:    0,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewRingBuffer(tt.args.size)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRingBuffer_Write(t *testing.T) {
	type args struct {
		v any
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "case1: write 1 into ringbuffer",
			args: args{v: 1},
		},
		{
			name: "case2: write 2 into ringbuffer",
			args: args{v: 2},
		},
	}
	r := NewRingBuffer(10)
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r.Write(tt.args.v)
			require.Equal(t, int32(i+1), r.Writer)
			require.Equal(t, tt.args.v, r.Container[i])
		})
	}
}

func TestRingBuffer_seekReader(t *testing.T) {
	type args struct {
		delta int32
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "case1: seek ringbuffer reader 1",
			args: args{delta: 1},
		},
		{
			name: "case2: seek ringbuffer reader 2",
			args: args{delta: 2},
		},
	}
	r := NewRingBuffer(5)
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r.seekReader(tt.args.delta)
			require.Equal(t, int32(i+1), r.Reader)
		})
	}
}

func TestRingBuffer_Read(t *testing.T) {
	type fields struct {
		Size      int32
		Container []any
		Reader    int32
		Writer    int32
	}
	tests := []struct {
		name   string
		fields fields
		want   any
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &RingBuffer{
				Size:      tt.fields.Size,
				Container: tt.fields.Container,
				Reader:    tt.fields.Reader,
				Writer:    tt.fields.Writer,
			}
			if got := r.Read(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("RingBuffer.Read() = %v, want %v", got, tt.want)
			}
		})
	}
}

// func TestRingBuffer_Latest(t *testing.T) {
// 	type fields struct {
// 		Size      int32
// 		Container []any
// 		Reader    int32
// 		Writer    int32
// 	}
// 	tests := []struct {
// 		name   string
// 		fields fields
// 		want   any
// 	}{
// 		// TODO: Add test cases.
// 	}
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			r := &RingBuffer{
// 				Size:      tt.fields.Size,
// 				Container: tt.fields.Container,
// 				Reader:    tt.fields.Reader,
// 				Writer:    tt.fields.Writer,
// 			}
// 			if got := r.Latest(); !reflect.DeepEqual(got, tt.want) {
// 				t.Errorf("RingBuffer.Latest() = %v, want %v", got, tt.want)
// 			}
// 		})
// 	}
// }

// func TestRingBuffer_Oldest(t *testing.T) {
// 	type fields struct {
// 		Size      int32
// 		Container []any
// 		Reader    int32
// 		Writer    int32
// 	}
// 	tests := []struct {
// 		name   string
// 		fields fields
// 		want   any
// 	}{
// 		// TODO: Add test cases.
// 	}
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			r := &RingBuffer{
// 				Size:      tt.fields.Size,
// 				Container: tt.fields.Container,
// 				Reader:    tt.fields.Reader,
// 				Writer:    tt.fields.Writer,
// 			}
// 			if got := r.Oldest(); !reflect.DeepEqual(got, tt.want) {
// 				t.Errorf("RingBuffer.Oldest() = %v, want %v", got, tt.want)
// 			}
// 		})
// 	}
// }

func TestRingBuffer_Overwrite(t *testing.T) {
	type fields struct {
		Size      int32
		Container []any
		Reader    int32
		Writer    int32
	}
	type args struct {
		v any
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &RingBuffer{
				Size:      tt.fields.Size,
				Container: tt.fields.Container,
				Reader:    tt.fields.Reader,
				Writer:    tt.fields.Writer,
			}
			r.Overwrite(tt.args.v)
		})
	}
}

func TestMultipleValues(t *testing.T) {
	rb := NewRingBuffer(2)
	rb.Write(2)
	rb.Write(3)
	rb.Write(4)

	a := rb.Read()
	b := rb.Read()

	if a != 4 || b != 3 {
		t.Errorf("Error Manipulating RingBuffer")
	}
}
