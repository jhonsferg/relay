// Package pool provides reusable byte buffer pools to reduce GC pressure.
package pool

import "sync"

// Multi-tier buffer pool sizes for efficient memory reuse by response size
const (
	smallBufferSize  = 4 * 1024      // 4 KB
	mediumBufferSize = 32 * 1024     // 32 KB
	largeBufferSize  = 256 * 1024    // 256 KB
	hugeBufferSize   = 1024 * 1024   // 1 MB
)

var (
	smallPool = &sync.Pool{
		New: func() any {
			buf := make([]byte, smallBufferSize)
			return &buf
		},
	}
	mediumPool = &sync.Pool{
		New: func() any {
			buf := make([]byte, mediumBufferSize)
			return &buf
		},
	}
	largePool = &sync.Pool{
		New: func() any {
			buf := make([]byte, largeBufferSize)
			return &buf
		},
	}
	hugePool = &sync.Pool{
		New: func() any {
			buf := make([]byte, hugeBufferSize)
			return &buf
		},
	}
)

// GetSizedBuffer returns a pooled byte slice sized appropriately for the given hint.
// hint should be the Content-Length (or estimated size) of the response body.
// The slice must be returned via PutSizedBuffer when done.
func GetSizedBuffer(hint int64) *[]byte {
	switch {
	case hint <= smallBufferSize:
		return smallPool.Get().(*[]byte)
	case hint <= mediumBufferSize:
		return mediumPool.Get().(*[]byte)
	case hint <= largeBufferSize:
		return largePool.Get().(*[]byte)
	default:
		return hugePool.Get().(*[]byte)
	}
}

// GetBuffer returns a pooled byte slice of at least mediumBufferSize (backward compat).
// Deprecated: Use GetSizedBuffer for better performance.
func GetBuffer() *[]byte {
	return mediumPool.Get().(*[]byte)
}

// PutSizedBuffer returns a buffer to the correct pool based on its capacity.
// Do not use the slice after calling this function.
func PutSizedBuffer(b *[]byte) {
	if b == nil {
		return
	}
	*b = (*b)[:0]

	switch cap(*b) {
	case smallBufferSize:
		smallPool.Put(b)
	case mediumBufferSize:
		mediumPool.Put(b)
	case largeBufferSize:
		largePool.Put(b)
	case hugeBufferSize:
		hugePool.Put(b)
	}
}

// PutBuffer returns a buffer to the pool (assumes mediumBufferSize).
// Deprecated: Use PutSizedBuffer for better performance.
func PutBuffer(b *[]byte) {
	if b != nil {
		*b = (*b)[:0]
		mediumPool.Put(b)
	}
}
