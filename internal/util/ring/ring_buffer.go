// Package ring, Simple RingBuffer pacakge
// from https://github.com/sahmad98/go-ringbuffer/blob/master/ring_buffer.go
//
// Ringbuffer is non blocking for readers and writers, writers will
// overwrite older data in a circular fashion. Readers will read
// from the current position and update it.
package ring

import "sync/atomic"

// RingBuffer Structure
type RingBuffer struct {
	Size      int32 // Size of the Ringbuffer
	Container []any // Array container of objects
	Reader    int32 // Reader position
	Writer    int32 // Writer Position
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
	current := atomic.LoadInt32(&r.Writer)
	r.Container[current] = v
	next := (current + 1) % r.Size
	atomic.StoreInt32(&r.Writer, next)
}

// Seek position of the reader by delta, delta can be positive as
// well as negative
func (r *RingBuffer) seekReader(delta int32) {
	current := atomic.LoadInt32(&r.Reader)
	expected := (current + delta) % r.Size
	atomic.StoreInt32(&r.Reader, expected)
}

// Read single object from the RingBuffer
func (r *RingBuffer) Read() any {
	defer r.seekReader(1)
	return r.Container[atomic.LoadInt32(&r.Reader)]
}

// Returns the latest element in the RingBuffer
func (r *RingBuffer) Latest() any {
	return r.Container[(atomic.LoadInt32(&r.Writer)-1)%r.Size]
}

// Returns the oldest element in RingBuffer
func (r *RingBuffer) Oldest() any {
	return r.Container[atomic.LoadInt32(&r.Writer)]
}

// Overwrites the latest data in RingBuffer
func (r *RingBuffer) Overwrite(v any) {
	r.Container[atomic.LoadInt32(&r.Writer)] = v
}
