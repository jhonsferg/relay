//go:build docker

// Integration tests against a real Memcached container, run via
// testcontainers-go. Opt-in (requires a local Docker daemon), skipped by
// default - run with `go test -tags=docker ./...`. memcached_test.go's
// fakeClient is a hand-rolled in-memory map, not even a real memcached
// protocol simulator - these tests exercise the real wire protocol via
// github.com/bradfitz/gomemcache against an actual memcached server.
package memcached_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/jhonsferg/relay"
	relaymemcached "github.com/jhonsferg/relay/ext/memcached"
)

// newDockerStore starts a real memcached container and returns a CacheStore
// wired to it. The container is terminated when the test finishes.
func newDockerStore(t *testing.T) *relaymemcached.CacheStore {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "memcached:1.6-alpine",
		ExposedPorts: []string{"11211/tcp"},
		WaitingFor:   wait.ForListeningPort("11211/tcp"),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("GenericContainer: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("container.Terminate: %v", err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container.Host: %v", err)
	}
	port, err := container.MappedPort(ctx, "11211/tcp")
	if err != nil {
		t.Fatalf("container.MappedPort: %v", err)
	}

	mc := memcache.New(fmt.Sprintf("%s:%s", host, port.Port()))
	return relaymemcached.NewCacheStore(mc, "relay:dockertest:")
}

func dockerSampleEntry(ttl time.Duration) *relay.CachedResponse {
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	return &relay.CachedResponse{
		StatusCode:   200,
		Status:       "200 OK",
		Headers:      http.Header{"Content-Type": {"application/json"}},
		Body:         []byte(`{"id":1}`),
		ExpiresAt:    exp,
		ETag:         `"v1"`,
		LastModified: "Mon, 01 Jan 2024 00:00:00 GMT",
	}
}

func TestDocker_CacheStore_SetAndGet(t *testing.T) {
	store := newDockerStore(t)

	store.Set("key1", dockerSampleEntry(time.Minute))
	got, ok := store.Get("key1")
	if !ok {
		t.Fatal("expected hit")
	}
	if got.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", got.StatusCode)
	}
	if string(got.Body) != `{"id":1}` {
		t.Errorf("Body = %q", string(got.Body))
	}
}

func TestDocker_CacheStore_TTLExpiry(t *testing.T) {
	store := newDockerStore(t)

	// Real memcached TTL is wall-clock (minimum 1s per encodeKey's rounding),
	// so use a short real TTL and sleep past it.
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
	store := newDockerStore(t)

	store.Set("del", dockerSampleEntry(time.Minute))
	if _, ok := store.Get("del"); !ok {
		t.Fatal("expected hit before delete")
	}
	store.Delete("del")
	if _, ok := store.Get("del"); ok {
		t.Error("expected miss after delete")
	}
}

func TestDocker_CacheStore_KeyEncodingHandlesSpecialChars(t *testing.T) {
	store := newDockerStore(t)

	// Cache keys contain colons and slashes, invalid as raw memcached keys -
	// this is exactly the kind of protocol-constraint bug a hand-rolled fake
	// (which never rejects malformed keys) can't catch.
	key := "GET:https://api.example.com/v1/users?page=1&limit=10"
	store.Set(key, dockerSampleEntry(time.Minute))

	got, ok := store.Get(key)
	if !ok {
		t.Fatal("expected hit for key with special characters")
	}
	if got.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", got.StatusCode)
	}
}

func TestDocker_CacheStore_Clear(t *testing.T) {
	store := newDockerStore(t)

	store.Set("a", dockerSampleEntry(time.Minute))
	store.Set("b", dockerSampleEntry(time.Minute))
	store.Clear()

	for _, k := range []string{"a", "b"} {
		if _, ok := store.Get(k); ok {
			t.Errorf("expected miss for %q after Clear", k)
		}
	}
}

func TestDocker_CacheStore_IntegrationWithRelayClient(t *testing.T) {
	store := newDockerStore(t)

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Cache-Control", "max-age=60")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"hits":%d}`, hits)
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithCache(store),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	resp1, err := c.Execute(c.Get("/resource"))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if hits != 1 {
		t.Errorf("hits after first request = %d, want 1", hits)
	}

	resp2, err := c.Execute(c.Get("/resource"))
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if hits != 1 {
		t.Errorf("hits after cached request = %d, want 1 (served from real memcached)", hits)
	}
	if resp1.String() != resp2.String() {
		t.Errorf("cached body mismatch: %q vs %q", resp1.String(), resp2.String())
	}
}
