//go:build docker

// Integration tests against a real Redis container, run via testcontainers-go.
// These are opt-in (require a local Docker daemon) and skipped by default -
// run with `go test -tags=docker ./...`. They cover the same core behavior as
// redis_test.go's miniredis-backed tests, but against real Redis so that
// TTL/SCAN/atomicity semantics miniredis only approximates are verified for
// real.
package redis_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	redisclient "github.com/redis/go-redis/v9"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/jhonsferg/relay"
	relayredis "github.com/jhonsferg/relay/ext/redis"
)

// newDockerTestStore starts a real Redis container and returns a CacheStore
// wired to it. The container is terminated when the test finishes.
func newDockerTestStore(t *testing.T) *relayredis.CacheStore {
	t.Helper()
	ctx := context.Background()

	container, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("tcredis.Run: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("container.Terminate: %v", err)
		}
	})

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("ConnectionString: %v", err)
	}
	opts, err := redisclient.ParseURL(connStr)
	if err != nil {
		t.Fatalf("ParseURL(%q): %v", connStr, err)
	}

	rdb := redisclient.NewClient(opts)
	t.Cleanup(func() { _ = rdb.Close() })

	return relayredis.NewCacheStore(rdb, "relay:dockertest:")
}

func dockerSampleEntry(ttl time.Duration) *relay.CachedResponse {
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}
	return &relay.CachedResponse{
		StatusCode:   200,
		Status:       "200 OK",
		Headers:      http.Header{"Content-Type": {"application/json"}},
		Body:         []byte(`{"id":1}`),
		ExpiresAt:    expiresAt,
		ETag:         `"abc123"`,
		LastModified: "Wed, 21 Oct 2015 07:28:00 GMT",
	}
}

func TestDocker_CacheStore_SetAndGet(t *testing.T) {
	store := newDockerTestStore(t)

	store.Set("key1", dockerSampleEntry(time.Minute))

	got, ok := store.Get("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", got.StatusCode)
	}
	if string(got.Body) != `{"id":1}` {
		t.Errorf("Body = %q, want {\"id\":1}", string(got.Body))
	}
	if got.ETag != `"abc123"` {
		t.Errorf("ETag = %q, want \"abc123\"", got.ETag)
	}
}

func TestDocker_CacheStore_TTLExpiry(t *testing.T) {
	store := newDockerTestStore(t)

	// Real Redis TTL is measured in wall-clock time (unlike miniredis's
	// FastForward), so use a short real TTL and sleep past it.
	store.Set("exp", dockerSampleEntry(1*time.Second))

	if _, ok := store.Get("exp"); !ok {
		t.Fatal("expected hit before expiry")
	}

	time.Sleep(1500 * time.Millisecond)

	if _, ok := store.Get("exp"); ok {
		t.Error("expected miss after real TTL expiry")
	}
}

func TestDocker_CacheStore_Delete(t *testing.T) {
	store := newDockerTestStore(t)

	store.Set("del-key", dockerSampleEntry(time.Minute))
	if _, ok := store.Get("del-key"); !ok {
		t.Fatal("expected hit before delete")
	}

	store.Delete("del-key")

	if _, ok := store.Get("del-key"); ok {
		t.Error("expected miss after delete")
	}
}

func TestDocker_CacheStore_ClearOnlyRemovesPrefixedKeys(t *testing.T) {
	store := newDockerTestStore(t)

	store.Set("shared", dockerSampleEntry(time.Minute))
	store.Clear()

	if _, ok := store.Get("shared"); ok {
		t.Error("expected miss after Clear")
	}
}

func TestDocker_CacheStore_IntegrationWithRelayClient(t *testing.T) {
	store := newDockerTestStore(t)

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Cache-Control", "max-age=60")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithCache(store),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	resp, err := c.Execute(c.Get("/resource"))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("first status = %d, want 200", resp.StatusCode)
	}
	if hits != 1 {
		t.Errorf("hits after first request = %d, want 1", hits)
	}

	resp, err = c.Execute(c.Get("/resource"))
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if hits != 1 {
		t.Errorf("hits after cached request = %d, want 1 (served from real Redis)", hits)
	}
	if body := resp.String(); body != `{"ok":true}` {
		t.Errorf("cached body = %q, want {\"ok\":true}", body)
	}
}
