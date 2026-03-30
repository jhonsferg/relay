// Package pool provides reusable byte buffer pools to reduce GC pressure.
package pool

import "sync"

const defaultBufferSize = 32 * 1024 // 32 KB

var bufPool = &sync.Pool{
	New: func() any {
		buf := make([]byte, defaultBufferSize)
		return &buf
	},
}

// GetBuffer returns a pooled byte slice of at least defaultBufferSize.
// The slice must be returned via PutBuffer when done.
func GetBuffer() *[]byte {
	return bufPool.Get().(*[]byte)
}

// PutBuffer returns a buffer to the pool. Do not use the slice after calling
// this function.
func PutBuffer(b *[]byte) {
	// Reset length to zero but keep capacity for reuse.
	if b != nil {
		*b = (*b)[:0]
		bufPool.Put(b)
	}
}
