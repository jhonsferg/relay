package relay

import (
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

// uuidV4Regexp matches a UUID v4 string:
// xxxxxxxx-xxxx-4xxx-[89ab]xxx-xxxxxxxxxxxx
var uuidV4Regexp = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

func TestGenerateIdempotencyKey_ValidUUIDv4Format(t *testing.T) {
	t.Parallel()

	for i := 0; i < 10; i++ {
		key, err := generateIdempotencyKey()
		if err != nil {
			t.Fatalf("generateIdempotencyKey() error: %v", err)
		}
		if !uuidV4Regexp.MatchString(key) {
			t.Errorf("key %q does not match UUID v4 format", key)
		}
	}
}

func TestGenerateIdempotencyKey_Unique(t *testing.T) {
	t.Parallel()

	const n = 100
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		key, err := generateIdempotencyKey()
		if err != nil {
			t.Fatalf("generateIdempotencyKey() error: %v", err)
		}
		if _, dup := seen[key]; dup {
			t.Errorf("duplicate key generated: %q", key)
		}
		seen[key] = struct{}{}
	}
}

func TestGenerateIdempotencyKey_Version4Bits(t *testing.T) {
	t.Parallel()

	key, err := generateIdempotencyKey()
	if err != nil {
		t.Fatalf("generateIdempotencyKey() error: %v", err)
	}

	// The 13th character (index 14: 8+1+4+1 = 14) is the version nibble, must be '4'.
	// UUID format: 8-4-4-4-12, dashes at positions 8, 13, 18, 23.
	// Version nibble is the first char of the third group.
	if len(key) < 19 {
		t.Fatalf("key too short: %q", key)
	}
	if key[14] != '4' {
		t.Errorf("version nibble should be '4', got %c in key %q", key[14], key)
	}
}

func TestWithAutoIdempotencyKey_HeaderInjected(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithAutoIdempotencyKey(),
	)

	_, err := c.Execute(c.Post(srv.URL() + "/create").WithBody([]byte("data")))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rec, err := srv.TakeRequest(time.Second)
	if err != nil {
		t.Fatalf("TakeRequest: %v", err)
	}

	key := rec.Headers.Get("X-Idempotency-Key")
	if key == "" {
		t.Error("expected X-Idempotency-Key header to be set")
	}
	if !uuidV4Regexp.MatchString(key) {
		t.Errorf("X-Idempotency-Key %q does not match UUID v4 format", key)
	}
}

func TestWithAutoIdempotencyKey_SameKeyOnRetries(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	// Enqueue 2 failures then a success (3 attempts total).
	srv.Enqueue(testutil.MockResponse{Status: http.StatusServiceUnavailable})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusServiceUnavailable})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(
		WithDisableCircuitBreaker(),
		WithAutoIdempotencyKey(),
		WithRetry(&RetryConfig{
			MaxAttempts:     3,
			InitialInterval: 1 * time.Millisecond,
			MaxInterval:     5 * time.Millisecond,
			Multiplier:      1.0,
			RandomFactor:    0,
			RetryableStatus: []int{http.StatusServiceUnavailable},
		}),
	)

	_, err := c.Execute(c.Post(srv.URL() + "/idem").WithBody([]byte("payload")))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Collect all 3 recorded requests.
	keys := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		req, err := srv.TakeRequest(time.Second)
		if err != nil {
			t.Fatalf("TakeRequest %d: %v", i, err)
		}
		keys = append(keys, req.Headers.Get("X-Idempotency-Key"))
	}

	if len(keys) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(keys))
	}

	// All retries must carry the same idempotency key.
	first := keys[0]
	if first == "" {
		t.Error("first request missing X-Idempotency-Key")
	}
	for i, k := range keys[1:] {
		if k != first {
			t.Errorf("retry %d has different idempotency key: want %q, got %q", i+1, first, k)
		}
	}
}

func TestWithIdempotencyKey_ManualKeyUsed(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())

	const manualKey = "my-custom-idem-key-123"
	req := c.Post(srv.URL() + "/op").
		WithBody([]byte("body")).
		WithIdempotencyKey(manualKey)

	_, err := c.Execute(req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rec, err := srv.TakeRequest(time.Second)
	if err != nil {
		t.Fatalf("TakeRequest: %v", err)
	}

	if rec.Headers.Get("X-Idempotency-Key") != manualKey {
		t.Errorf("expected X-Idempotency-Key=%q, got %q",
			manualKey, rec.Headers.Get("X-Idempotency-Key"))
	}
}

func TestIdempotencyKeyHeader_ConstantValue(t *testing.T) {
	t.Parallel()
	if idempotencyKeyHeader != "X-Idempotency-Key" {
		t.Errorf("idempotencyKeyHeader should be 'X-Idempotency-Key', got %q", idempotencyKeyHeader)
	}
}
