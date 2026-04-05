# TLS Configuration

Transport Layer Security (TLS) is the foundation of secure HTTP communication. `relay` exposes fine-grained control over TLS configuration, including custom certificate authorities, client certificates for mutual TLS (mTLS), and live certificate rotation without restarting the process.

For most public-internet use cases, the default TLS configuration (which uses the system certificate pool) is sufficient. The APIs described here are for situations where you need custom trust roots, client authentication, or operational features like zero-downtime certificate rotation.

---

## WithTLSConfig

```go
func WithTLSConfig(cfg *tls.Config) Option
```

Replaces the default TLS configuration with a fully custom `*tls.Config`. This is the most powerful option - it gives you direct control over every aspect of the TLS handshake.

```go
package main

import (
    "context"
    "crypto/tls"
    "crypto/x509"
    "fmt"
    "log"
    "os"

    "github.com/jhonsferg/relay"
)

func main() {
    // Load a custom CA certificate (e.g., your internal PKI root)
    caCert, err := os.ReadFile("/etc/ssl/internal-ca.crt")
    if err != nil {
        log.Fatal("read CA cert:", err)
    }

    caCertPool := x509.NewCertPool()
    if !caCertPool.AppendCertsFromPEM(caCert) {
        log.Fatal("failed to parse CA certificate")
    }

    tlsConfig := &tls.Config{
        RootCAs:    caCertPool,
        MinVersion: tls.VersionTLS13,
    }

    client, err := relay.New(
        relay.WithBaseURL("https://internal-api.corp.example.com"),
        relay.WithTLSConfig(tlsConfig),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/health", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```

> **tip**
> Always set `MinVersion: tls.VersionTLS13` in production. TLS 1.0 and 1.1 are deprecated and vulnerable to known attacks. TLS 1.2 is acceptable but TLS 1.3 provides better security and performance (zero round-trip resumption).

---

## WithDynamicTLSCert

```go
func WithDynamicTLSCert(certFile, keyFile string) (*CertWatcher, error)
```

`WithDynamicTLSCert` watches `certFile` and `keyFile` on disk and automatically reloads them when they change. This is essential for services using automated certificate management (e.g., cert-manager, Vault PKI, AWS ACM PCA) where certificates are rotated regularly without a process restart.

Returns a `CertWatcher` that you can use to stop the background watcher goroutine when it is no longer needed.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    watcher, err := relay.WithDynamicTLSCert(
        "/etc/ssl/client.crt",
        "/etc/ssl/client.key",
    )
    if err != nil {
        log.Fatal("create cert watcher:", err)
    }

    client, err := relay.New(
        relay.WithBaseURL("https://secure-api.internal"),
        relay.WithCertWatcher(watcher),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Clean up the watcher on shutdown
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    go func() {
        <-sigCh
        log.Println("shutting down, stopping cert watcher")
        watcher.Stop()
        os.Exit(0)
    }()

    // Main loop - certificate rotations are handled transparently
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    for range ticker.C {
        resp, err := client.Get(context.Background(), "/data", nil)
        if err != nil {
            log.Printf("request failed: %v", err)
            continue
        }
        resp.Body.Close()
        fmt.Println("ok, status:", resp.StatusCode)
    }
}
```

---

## CertWatcher Type and CertWatcher.Stop

```go
type CertWatcher struct { /* ... */ }

// Stop terminates the background goroutine that watches for certificate changes.
// After Stop returns, no further reloads will occur.
func (w *CertWatcher) Stop()
```

`CertWatcher` is the handle returned by `WithDynamicTLSCert`. It runs a background goroutine that polls the certificate files for changes (detected via file modification time or a filesystem event). When a change is detected, the new certificate and key are loaded and the TLS configuration is updated atomically.

```go
package main

import (
    "log"

    "github.com/jhonsferg/relay"
)

func setupClient() (*relay.Client, func(), error) {
    watcher, err := relay.WithDynamicTLSCert(
        "/run/secrets/tls/tls.crt",
        "/run/secrets/tls/tls.key",
    )
    if err != nil {
        return nil, nil, err
    }

    client, err := relay.New(
        relay.WithBaseURL("https://partner-api.example.com"),
        relay.WithCertWatcher(watcher),
    )
    if err != nil {
        watcher.Stop()
        return nil, nil, err
    }

    cleanup := func() {
        log.Println("stopping cert watcher")
        watcher.Stop()
    }

    return client, cleanup, nil
}

func main() {
    client, cleanup, err := setupClient()
    if err != nil {
        log.Fatal(err)
    }
    defer cleanup()

    log.Println("client ready:", client)
    // ... use client
}
```

---

## WithCertWatcher

```go
func WithCertWatcher(watcher *CertWatcher) Option
```

Attaches a `CertWatcher` (created by `WithDynamicTLSCert`) to the client. The client will use whatever certificate the watcher has most recently loaded. New TLS connections will use the latest certificate; existing persistent connections are unaffected until they are re-established.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    watcher, err := relay.WithDynamicTLSCert(
        "/var/run/certs/service.crt",
        "/var/run/certs/service.key",
    )
    if err != nil {
        log.Fatal(err)
    }
    defer watcher.Stop()

    client, err := relay.New(
        relay.WithBaseURL("https://partner.example.com"),
        relay.WithCertWatcher(watcher),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/api/v1/status", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```

---

## Certificate Rotation in Long-Running Services

In Kubernetes with cert-manager, certificates are stored in Secrets and mounted into pods as files. cert-manager renews certificates before they expire and updates the Secret (and thus the mounted files) automatically. Without dynamic cert loading, your service would continue using the old certificate until the pod restarts.

With `WithDynamicTLSCert`, the rotation happens in-process:

```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/jhonsferg/relay"
)

// CertPaths holds the filesystem paths for TLS material.
// In Kubernetes, these are typically projected from a Secret.
type CertPaths struct {
    CertFile string
    KeyFile  string
    CAFile   string
}

func newProductionClient(certs CertPaths) (*relay.Client, func(), error) {
    watcher, err := relay.WithDynamicTLSCert(certs.CertFile, certs.KeyFile)
    if err != nil {
        return nil, nil, err
    }

    client, err := relay.New(
        relay.WithBaseURL("https://downstream-service.prod.svc.cluster.local"),
        relay.WithCertWatcher(watcher),
        relay.WithTimeout(10*time.Second),
        relay.WithRetry(relay.RetryConfig{
            MaxAttempts: 3,
            Backoff:     relay.ExponentialBackoff(100*time.Millisecond, 2.0),
        }),
    )
    if err != nil {
        watcher.Stop()
        return nil, nil, err
    }

    return client, watcher.Stop, nil
}

func main() {
    certs := CertPaths{
        // These are the default paths when cert-manager mounts a Certificate
        // as a volume with secretName in a Kubernetes Pod spec.
        CertFile: "/etc/tls/tls.crt",
        KeyFile:  "/etc/tls/tls.key",
        CAFile:   "/etc/tls/ca.crt",
    }

    client, stop, err := newProductionClient(certs)
    if err != nil {
        log.Fatal(err)
    }
    defer stop()

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            resp, err := client.Get(context.Background(), "/health", nil)
            if err != nil {
                log.Printf("health check failed: %v", err)
                continue
            }
            resp.Body.Close()
            log.Println("health check ok")
        case <-sigCh:
            log.Println("shutting down")
            return
        }
    }
}
```

> **note**
> When certificates are rotated, only new TCP connections use the new certificate. Existing keep-alive connections continue using the old certificate until they are closed. To ensure all connections use the new certificate promptly, you can configure `relay` with a shorter `MaxIdleConnDuration` or call `CloseIdleConnections()` on the client after a rotation event.

---

## Mutual TLS (mTLS) Example

Mutual TLS requires both the client and server to present certificates. This is common in zero-trust network architectures and service meshes (e.g., Istio, Linkerd).

```go
package main

import (
    "context"
    "crypto/tls"
    "crypto/x509"
    "fmt"
    "log"
    "os"

    "github.com/jhonsferg/relay"
)

func loadMTLSConfig(
    clientCertFile string,
    clientKeyFile  string,
    caCertFile     string,
) (*tls.Config, error) {
    // Load client certificate and key
    clientCert, err := tls.LoadX509KeyPair(clientCertFile, clientKeyFile)
    if err != nil {
        return nil, fmt.Errorf("load client cert: %w", err)
    }

    // Load the CA certificate that signed the server's certificate
    caCert, err := os.ReadFile(caCertFile)
    if err != nil {
        return nil, fmt.Errorf("read CA cert: %w", err)
    }
    caPool := x509.NewCertPool()
    if !caPool.AppendCertsFromPEM(caCert) {
        return nil, fmt.Errorf("parse CA cert: invalid PEM")
    }

    return &tls.Config{
        Certificates: []tls.Certificate{clientCert},
        RootCAs:      caPool,
        MinVersion:   tls.VersionTLS13,
    }, nil
}

func main() {
    tlsConfig, err := loadMTLSConfig(
        "/etc/mtls/client.crt",
        "/etc/mtls/client.key",
        "/etc/mtls/ca.crt",
    )
    if err != nil {
        log.Fatal(err)
    }

    client, err := relay.New(
        relay.WithBaseURL("https://internal-gateway.corp.example.com"),
        relay.WithTLSConfig(tlsConfig),
    )
    if err != nil {
        log.Fatal(err)
    }

    // The TLS handshake will present the client certificate.
    // The server will reject the connection if the client cert is not trusted.
    resp, err := client.Get(context.Background(), "/secure-resource", nil)
    if err != nil {
        log.Fatal("mTLS request failed:", err)
    }
    defer resp.Body.Close()
    fmt.Println("mTLS request successful, status:", resp.StatusCode)
}
```

For mTLS with dynamic certificate rotation (e.g., SPIFFE/SPIRE workload API):

```go
package main

import (
    "context"
    "crypto/tls"
    "crypto/x509"
    "fmt"
    "log"
    "os"

    "github.com/jhonsferg/relay"
)

func newMTLSClientWithRotation() (*relay.Client, func(), error) {
    watcher, err := relay.WithDynamicTLSCert(
        "/run/spire/bundle/client.crt",
        "/run/spire/bundle/client.key",
    )
    if err != nil {
        return nil, nil, fmt.Errorf("create cert watcher: %w", err)
    }

    caCert, err := os.ReadFile("/run/spire/bundle/ca.crt")
    if err != nil {
        watcher.Stop()
        return nil, nil, fmt.Errorf("read CA: %w", err)
    }
    caPool := x509.NewCertPool()
    caPool.AppendCertsFromPEM(caCert)

    client, err := relay.New(
        relay.WithBaseURL("https://partner-service.mesh"),
        relay.WithCertWatcher(watcher),
        relay.WithTLSConfig(&tls.Config{
            RootCAs:    caPool,
            MinVersion: tls.VersionTLS13,
        }),
    )
    if err != nil {
        watcher.Stop()
        return nil, nil, err
    }

    return client, watcher.Stop, nil
}

func main() {
    client, stop, err := newMTLSClientWithRotation()
    if err != nil {
        log.Fatal(err)
    }
    defer stop()

    resp, err := client.Get(context.Background(), "/api/v1/data", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```

---

## Skipping TLS Verification (Development Only)

> **warning**
> **Never** disable TLS verification in production. `InsecureSkipVerify: true` means your client will connect to any server, including those presenting fraudulent certificates. This completely defeats the purpose of TLS and makes your service vulnerable to man-in-the-middle attacks. Use this only in local development environments or isolated test networks where you understand the security implications.

```go
package main

import (
    "context"
    "crypto/tls"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

// newDevClient creates a client suitable for local development against
// self-signed certificates. DO NOT USE IN PRODUCTION.
func newDevClient(baseURL string) (*relay.Client, error) {
    // nolint:gosec // InsecureSkipVerify is intentional for local dev only
    devTLSConfig := &tls.Config{
        InsecureSkipVerify: true, //nolint:gosec
    }

    return relay.New(
        relay.WithBaseURL(baseURL),
        relay.WithTLSConfig(devTLSConfig),
    )
}

func main() {
    // Only safe because this points at a local dev server
    client, err := newDevClient("https://localhost:8443")
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/health", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("dev server status:", resp.StatusCode)
}
```

A better approach for development is to generate a self-signed certificate and add it to your local trust store, or use `mkcert` to create a locally-trusted development certificate:

```bash
# Install mkcert and create a locally trusted certificate
mkcert -install
mkcert localhost 127.0.0.1
# Then use the generated files with WithTLSConfig + a custom RootCAs pool
# This avoids InsecureSkipVerify entirely
```

---

## Summary

| Scenario | API |
|---|---|
| Custom CA / cipher suites | `WithTLSConfig(&tls.Config{...})` |
| Live cert rotation | `WithDynamicTLSCert(certFile, keyFile)` + `WithCertWatcher(w)` |
| Mutual TLS | `WithTLSConfig` with `Certificates` field populated |
| mTLS with rotation | `WithCertWatcher` + `WithTLSConfig` for CA pool |
| Stop watcher goroutine | `watcher.Stop()` |
| Development self-signed | `WithTLSConfig` with `InsecureSkipVerify: true` (dev only) |
