# WASM / js Build Support

relay's core HTTP client compiles to `GOOS=js GOARCH=wasm` and runs in any environment that provides the Go `syscall/js` package  -  WebAssembly runtimes, Wasm Edge, and browsers. The Go standard library's `net/http` package transparently delegates to the browser's [Fetch API](https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API) when running in a browser context.

---

## Build Command

```bash
GOOS=js GOARCH=wasm go build -o app.wasm ./cmd/myapp
```

Or cross-compile from a non-js host:

```bash
GOOS=js GOARCH=wasm go build -o app.wasm .
```

---

## What Works

| Feature | WASM support |
|---------|-------------|
| Core HTTP (GET, POST, PUT, PATCH, DELETE) | ✅ |
| JSON encoding / decoding | ✅ |
| Request/response middleware | ✅ |
| Retries, timeouts, circuit breaker | ✅ |
| Schema validation | ✅ |
| HAR recording | ✅ |
| SRV discovery | ✅ |
| HTTP/2 push handler | ✅ (stored; no-op at runtime) |
| Credential providers | ✅ |
| TLS config (custom CA, mTLS) | ⚠️ Delegated to the Fetch API; custom `*tls.Config` is ignored |
| Unix socket (`WithUnixSocket`) | ❌ Not available (see note) |

---

## Usage

```go
//go:build js

package main

import (
    "context"
    "fmt"
    "syscall/js"

    "github.com/jhonsferg/relay"
)

func main() {
    client, err := relay.New(relay.WithBaseURL("https://api.example.com"))
    if err != nil {
        panic(err)
    }

    // Expose a JS-callable function.
    js.Global().Set("fetchUser", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
        go func() {
            type User struct {
                ID   int    `json:"id"`
                Name string `json:"name"`
            }
            user, _, err := relay.Get[User](context.Background(), client, "/users/1", nil)
            if err != nil {
                fmt.Println("error:", err)
                return
            }
            fmt.Printf("user: %+v\n", user)
        }()
        return nil
    }))

    // Block forever so the Wasm module stays alive.
    select {}
}
```

Load in an HTML page using the standard Go Wasm loader:

```html
<script src="wasm_exec.js"></script>
<script>
  const go = new Go();
  WebAssembly.instantiateStreaming(fetch("app.wasm"), go.importObject)
    .then(result => go.run(result.instance));
</script>
```

---

## Notes

- **Unix sockets** (`WithUnixSocket`) are guarded by a `//go:build !js` tag and will not compile for the `js` target. Remove it from your client configuration when building for WASM.
- **TLS configuration** passed via `WithTLSConfig` is silently ignored in the browser; TLS is handled entirely by the Fetch API, which follows the browser's certificate trust store.
- The Fetch API enforces CORS; cross-origin requests require appropriate `Access-Control-Allow-*` headers from the server.
- `GOARCH=wasm` produces a single-threaded event loop; use goroutines for concurrent requests but be aware that blocking syscalls are not available.
