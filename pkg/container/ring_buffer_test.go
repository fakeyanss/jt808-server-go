// Package ring, Simple RingBuffer pacakge
// from https://github.com/sahmad98/go-ringbuffer/blob/master/ring_buffer.go
//
// Ringbuffer is non blocking for readers and writers, writers will
// overwrite older data in a circular fashion. Readers will read
// from the current position and update it.
package container

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

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
	var idx int32
	for i, tt := range tests {
		idx = idx + int32(i) + 1
		t.Run(tt.name, func(t *testing.T) {
			r.seekReader(tt.args.delta)
			require.Equal(t, idx, r.Reader)
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

// 并发写入与读取必须无数据竞争（在 -race 下运行生效）。
// 修复前该测试会报 DATA RACE：Write 直接写 Container 元素，
// Latest/Read/Oldest 无同步地读同一数组。
func TestRingBufferConcurrentWriteRead(t *testing.T) {
	rb := NewRingBuffer(64)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-ctx.Done():
					return
				default:
					rb.Write(id<<20 + i)
				}
			}
		}(w)
	}
	for r := 0; r < 2; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					_ = rb.Latest()
					_ = rb.Read()
					_ = rb.Oldest()
				}
			}
		}()
	}
	time.Sleep(200 * time.Millisecond)
	cancel()
	wg.Wait()
}

// 负 delta 寻址必须环绕到正索引，不得产生负下标导致越界 panic。
func TestRingBufferSeekReaderNegativeDelta(t *testing.T) {
	rb := NewRingBuffer(5)
	for i := 0; i < 5; i++ {
		rb.Write(i)
	}
	rb.seekReader(1)  // Reader: 0 → 1
	rb.seekReader(-3) // 1-3=-2，修复后环绕为 3
	require.Equal(t, int32(3), rb.Reader)
	require.Equal(t, 3, rb.Read()) // 读 Container[3]，Reader 推进到 4
	require.Equal(t, int32(4), rb.Reader)
	rb.seekReader(-6) // 4-6=-2，环绕为 3
	require.Equal(t, int32(3), rb.Reader)
	rb.seekReader(-8) // 3-8=-5，-5%5=0
	require.Equal(t, int32(0), rb.Reader)
}
