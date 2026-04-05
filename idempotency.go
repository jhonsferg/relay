package relay

import (
	"crypto/rand"
	"fmt"
	"net/http"
)

const idempotencyKeyHeader = "X-Idempotency-Key"

const hexChars = "0123456789abcdef"

// generateIdempotencyKey returns a new UUID v4-like random key.
// Uses stack-allocated buffers to produce zero heap allocations beyond
// the single string conversion at the end.
func generateIdempotencyKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("relay: generate idempotency key: %w", err)
	}
	// Set UUID v4 version and variant bits.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	// Encode directly into a stack-allocated 36-byte buffer:
	// xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	var buf [36]byte
	i := 0
	for j, c := range b {
		if j == 4 || j == 6 || j == 8 || j == 10 {
			buf[i] = '-'
			i++
		}
		buf[i] = hexChars[c>>4]
		buf[i+1] = hexChars[c&0x0f]
		i += 2
	}
	return string(buf[:]), nil
}

// isSafeMethod reports whether method is semantically idempotent or safe
// per RFC 9110: GET, HEAD, PUT, OPTIONS, and TRACE. POST, PATCH, and DELETE
// are excluded because they are not guaranteed safe to replay.
func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut,
		http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}
