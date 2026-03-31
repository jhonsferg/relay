package pool

import (
	"bytes"
	"sync"
)

var bytesReaderPool = &sync.Pool{
	New: func() any { return &bytes.Reader{} },
}

// GetBytesReader returns a pooled bytes.Reader initialised with the given bytes.
// The reader must be returned via PutBytesReader when done.
func GetBytesReader(b []byte) *bytes.Reader {
	r := bytesReaderPool.Get().(*bytes.Reader)
	r.Reset(b)
	return r
}

// PutBytesReader returns a bytes.Reader to the pool. Must be called after
// the reader is no longer needed to allow reuse. Do not use the reader
// after calling this function.
func PutBytesReader(r *bytes.Reader) {
	if r != nil {
		r.Reset(nil)
		bytesReaderPool.Put(r)
	}
}
