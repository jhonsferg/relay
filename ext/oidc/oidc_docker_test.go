//go:build docker

// Integration test against a real Keycloak identity provider, run via
// testcontainers-go. Opt-in (requires a local Docker daemon), skipped by
// default - run with `go test -tags=docker ./...`.
//
// oidc.go's RefreshingTokenSource delegates the actual client-credentials
// token exchange to golang.org/x/oauth2/clientcredentials (independently
// tested upstream), so this isn't testing new code paths - it's an
// end-to-end confidence check that this package's wiring of that library
// (context/timeout handling, TokenSource adaptation) works against a real,
// widely-deployed OIDC provider, not just a synthetic httptest server.
package oidc_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/ext/oidc"
)

const (
	kcRealm        = "relay-test"
	kcClientID     = "relay-test-client"
	kcClientSecret = "relay-test-secret"
)

// kcRealmExport is a minimal Keycloak realm pre-configured with a
// confidential client that has the service-account (client_credentials)
// grant enabled, imported automatically at container startup. Keycloak
// requires the import filename to be "<realm>-realm.json".
const kcRealmExport = `{
  "realm": "` + kcRealm + `",
  "enabled": true,
  "clients": [
    {
      "clientId": "` + kcClientID + `",
      "enabled": true,
      "protocol": "openid-connect",
      "publicClient": false,
      "secret": "` + kcClientSecret + `",
      "serviceAccountsEnabled": true,
      "standardFlowEnabled": false,
      "directAccessGrantsEnabled": false,
      "clientAuthenticatorType": "client-secret"
    }
  ]
}`

// startKeycloak starts a real Keycloak container pre-loaded with the
// relay-test realm/client above and returns its base URL
// ("http://host:port"). The container is terminated when the test finishes.
func startKeycloak(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "quay.io/keycloak/keycloak:latest",
		ExposedPorts: []string{"8080/tcp"},
		Env: map[string]string{
			"KC_BOOTSTRAP_ADMIN_USERNAME": "admin",
			"KC_BOOTSTRAP_ADMIN_PASSWORD": "admin",
			"KEYCLOAK_ADMIN":              "admin",
			"KEYCLOAK_ADMIN_PASSWORD":     "admin",
		},
		Cmd: []string{"start-dev", "--import-realm"},
		Files: []testcontainers.ContainerFile{
			{
				Reader:            strings.NewReader(kcRealmExport),
				ContainerFilePath: "/opt/keycloak/data/import/" + kcRealm + "-realm.json",
				FileMode:          0o444,
			},
		},
		WaitingFor: wait.ForHTTP("/realms/" + kcRealm).
			WithPort("8080/tcp").
			WithStartupTimeout(3 * time.Minute),
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
	port, err := container.MappedPort(ctx, "8080/tcp")
	if err != nil {
		t.Fatalf("container.MappedPort: %v", err)
	}
	return fmt.Sprintf("http://%s:%s", host, port.Port())
}

func TestDocker_RefreshingTokenSource_RealKeycloak(t *testing.T) {
	base := startKeycloak(t)
	tokenURL := base + "/realms/" + kcRealm + "/protocol/openid-connect/token"

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	src := oidc.RefreshingTokenSourceContextTimeout(context.Background(), kcClientID, kcClientSecret, tokenURL, 15*time.Second)
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		oidc.WithBearerToken(src),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(15*time.Second),
	)

	resp, err := client.Execute(client.Get("/protected"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Fatalf("Authorization header = %q, want Bearer <token> from real Keycloak", gotAuth)
	}
	if len(strings.TrimPrefix(gotAuth, "Bearer ")) < 20 {
		t.Errorf("token looks too short to be a real Keycloak-issued JWT: %q", gotAuth)
	}
}

func TestDocker_RefreshingTokenSource_WrongSecretRejected(t *testing.T) {
	base := startKeycloak(t)
	tokenURL := base + "/realms/" + kcRealm + "/protocol/openid-connect/token"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	src := oidc.RefreshingTokenSourceContextTimeout(context.Background(), kcClientID, "not-the-real-secret", tokenURL, 15*time.Second)
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		oidc.WithBearerToken(src),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(15*time.Second),
	)

	_, err := client.Execute(client.Get("/protected"))
	if err == nil {
		t.Fatal("expected an error from real Keycloak rejecting the wrong client secret, got nil")
	}
}
