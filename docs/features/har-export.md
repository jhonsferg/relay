# HAR Export

`HARRecorder` captures every HTTP request/response pair made by a relay client and serialises them as a [HAR 1.2](https://w3c.github.io/web-performance/specs/HAR/Overview.html) document. Use it for debugging, API traffic analysis, test fixtures, or feeding requests into tools like Postman, Charles Proxy, and browser DevTools.

---

## Usage

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/jhonsferg/relay"
)

func main() {
    rec := relay.NewHARRecorder("my-service", "1.0.0")

    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTransportMiddleware(rec.Middleware()),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Make some requests.
    ctx := context.Background()
    client.Get(ctx, "/users", nil)       //nolint:errcheck
    client.Get(ctx, "/products", nil)    //nolint:errcheck

    fmt.Println("Recorded entries:", rec.EntryCount())

    // Iterate over entries using the Go 1.23 range-over-func iterator.
    for entry := range rec.All() {
        fmt.Printf("  %s %s → %d\n",
            entry.Request.Method,
            entry.Request.URL,
            entry.Response.Status,
        )
    }

    // Export to a HAR file.
    data, err := rec.ExportJSON()
    if err != nil {
        log.Fatal(err)
    }
    if err := os.WriteFile("traffic.har", data, 0o600); err != nil {
        log.Fatal(err)
    }

    // Reset between test cases.
    rec.Reset()
}
```

---

## API Reference

### `NewHARRecorder`

```go
func NewHARRecorder(args ...string) *HARRecorder
```

Creates a new, empty recorder. The optional variadic arguments set the creator name and version that appear in the HAR `creator` object (defaults: `"relay"`, `"0.1.0"`).

### `Middleware`

```go
func (r *HARRecorder) Middleware() func(http.RoundTripper) http.RoundTripper
```

Returns a relay-compatible transport middleware. Pass it to `relay.WithTransportMiddleware` when constructing the client.

### `ExportJSON`

```go
func (r *HARRecorder) ExportJSON() ([]byte, error)
```

Returns the full HAR 1.2 document as pretty-printed JSON. Thread-safe; can be called while recording is ongoing.

### `ExportHAR`

```go
func (r *HARRecorder) ExportHAR() *HAR
```

Returns the recorded transactions as a `*HAR` struct (useful when you want to inspect or manipulate the data programmatically before serialising).

### `EntryCount`

```go
func (r *HARRecorder) EntryCount() int
```

Returns the number of recorded entries. Thread-safe.

### `All`

```go
func (r *HARRecorder) All() iter.Seq[HAREntry]
```

Returns an iterator over a snapshot of entries. Requires Go 1.23+.

```go
for entry := range recorder.All() {
    // process entry
}
```

### `Entries`

```go
func (r *HARRecorder) Entries() []HAREntry
```

Returns a copy of all recorded entries as a slice.

### `Reset`

```go
func (r *HARRecorder) Reset()
```

Clears all recorded entries. Useful between test cases.

---

## Data Structures

| Type | Description |
|------|-------------|
| `HAREntry` | A single request/response pair with timing info |
| `HARRequest` | Method, URL, headers, query params, body |
| `HARResponse` | Status, headers, body content, MIME type |
| `HARTimings` | Send / wait / receive breakdown in milliseconds |
| `HARContent` | Response body text and MIME type |
| `HARPostData` | Request body text and MIME type |
| `HAR` / `HARLog` | Top-level document wrapping all entries |

---

## Notes

- `HARRecorder` is **safe for concurrent use**; all methods acquire an internal mutex.
- Response bodies are fully buffered during recording so they can be captured  -  this adds memory overhead proportional to response size.
- `ExportJSON` / `ExportHAR` snapshot the entries at call time; entries added concurrently after the call are not included.
