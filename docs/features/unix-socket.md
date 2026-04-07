# Unix Socket Transport

`WithUnixSocket` routes all HTTP traffic through a Unix domain socket instead of a TCP connection. This is useful for communicating with local daemons that expose a Unix socket API — most notably the Docker daemon at `/var/run/docker.sock`.

---

## Usage

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    // Talk to the local Docker daemon over its Unix socket.
    client, err := relay.New(
        relay.WithBaseURL("http://localhost"), // host header; path is used normally
        relay.WithUnixSocket("/var/run/docker.sock"),
    )
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()

    // List running containers.
    resp, err := client.Get(ctx, "/containers/json", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    var containers []map[string]any
    if err := json.NewDecoder(resp.Body).Decode(&containers); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("running containers: %d\n", len(containers))
}
```

---

## API Reference

### `WithUnixSocket`

```go
func WithUnixSocket(socketPath string) Option
```

Configures the relay client to dial `socketPath` for every request, regardless of the host in the request URL. The `baseURL` still controls the `Host` header and the URL path; only the transport layer is changed.

---

## Notes

- **Not available on Windows or WASM.** The function is guarded by a `//go:build !js` tag. On Windows, Unix domain socket support requires Windows 10 Build 17063+ and is not yet exposed by this option.
- The `baseURL` scheme and host are used as normal for the `Host` header and path construction; only the underlying TCP dial is replaced with a Unix socket dial.
- Combine with other relay options such as retries, timeouts, and middleware — they all work transparently on top of the Unix transport.
- Ensure the process has read/write permission on the socket file (e.g. add the user to the `docker` group for `/var/run/docker.sock`).
