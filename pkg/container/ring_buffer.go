// Package ring, Simple RingBuffer pacakge
// from https://github.com/sahmad98/go-ringbuffer/blob/master/ring_buffer.go
//
// Ringbuffer is non blocking for readers and writers, writers will
// overwrite older data in a circular fashion. Readers will read
// from the current position and update it.
package container

import "sync"

// RingBuffer Structure
type RingBuffer struct {
	Size      int32 // Size of the Ringbuffer
	Container []any // Array container of objects
	Reader    int32 // Reader position
	Writer    int32 // Writer Position

	// mu 保护 Container 元素与读写位置：此前仅位置变量使用 atomic，
	// 元素读写无同步，并发 Write/Latest 存在数据竞争。
	mu sync.Mutex
}

// Create a new RingBuffer of initial size "size"
// Returns a pointer to the new RingBuffer
func NewRingBuffer(size int32) *RingBuffer {
	rb := new(RingBuffer)
	rb.Size = size
	rb.Container = make([]any, size)
	rb.Reader = 0
	rb.Writer = 0
	return rb
}

// Write object into the RingBuffer
func (r *RingBuffer) Write(v any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Container[r.Writer] = v
	r.Writer = (r.Writer + 1) % r.Size
}

// seekReader 按 delta 移动读位置，delta 可为正或负。
// 调用前必须持有 r.mu（由 Read 等公开方法进入）。
func (r *RingBuffer) seekReader(delta int32) {
	expected := (r.Reader + delta) % r.Size
	if expected < 0 {
		// Go 取模结果符号跟随被除数，负 delta 会得到负索引，需环绕回正区间。
		expected += r.Size
	}
	r.Reader = expected
}

// Read single object from the RingBuffer
func (r *RingBuffer) Read() any {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.Container[r.Reader]
	r.seekReader(1)
	return v
}

// Returns the latest element in the RingBuffer
func (r *RingBuffer) Latest() any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Writer == 0 {
		return nil
	}
	return r.Container[(r.Writer-1)%r.Size]
}

// Returns the oldest element in RingBuffer
func (r *RingBuffer) Oldest() any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Container[r.Writer]
}

// Overwrites the latest data in RingBuffer
func (r *RingBuffer) Overwrite(v any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Container[r.Writer] = v
}
