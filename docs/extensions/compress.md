# Compress Extension

The `ext/compress` module provides zstd compression helpers for relay clients, including support for pre-trained dictionaries that dramatically improve compression ratios for small, structurally similar payloads (e.g. JSON API responses with a consistent schema).

---

## Installation

```bash
go get github.com/jhonsferg/relay/ext/compress@latest
```

The extension lives in its own Go module (`github.com/jhonsferg/relay/ext/compress`) to keep the zstd dependency out of the core relay module.

---

## Usage

### Standard zstd (no dictionary)

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
    "github.com/jhonsferg/relay/ext/compress"
)

func main() {
    opt, err := compress.WithZstdDictionary(nil) // nil = no dictionary
    if err != nil {
        log.Fatal(err)
    }

    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        opt,
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/data", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```

### Dictionary-Compressed zstd

Pre-trained dictionaries are most useful when your payloads share a common structure. Train a dictionary from a representative sample of your traffic using the `zstd` CLI:

```bash
# Collect ~100–1000 representative JSON samples
mkdir samples && for i in $(seq 1 200); do
    curl -s https://api.example.com/users/$i > samples/user_$i.json
done

# Train the dictionary (target size: 112 KiB)
zstd --train samples/*.json -o user_dict.zstd
```

Then load the dictionary at startup:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/jhonsferg/relay"
    "github.com/jhonsferg/relay/ext/compress"
)

func main() {
    dict, err := os.ReadFile("user_dict.zstd")
    if err != nil {
        log.Fatal("read dict:", err)
    }

    opt, err := compress.WithZstdDictionary(dict)
    if err != nil {
        log.Fatal("create compressor:", err)
    }

    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        opt,
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/users/1", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```

### Using `ZstdDictCompressor` directly

```go
comp, err := compress.NewZstdDictionaryCompressor(dict)
if err != nil {
    log.Fatal(err)
}

compressed, err := comp.Compress([]byte(`{"id":1,"name":"Alice"}`))
if err != nil {
    log.Fatal(err)
}

original, err := comp.Decompress(compressed)
if err != nil {
    log.Fatal(err)
}
fmt.Println(string(original))
```

---

## API Reference

### `WithZstdDictionary`

```go
func WithZstdDictionary(dict []byte) (relay.Option, error)
```

Returns a relay `Option` that installs a transport middleware performing:
- **Request**: compresses the body with zstd (+ dictionary if provided) and sets `Content-Encoding: zstd`.
- **Response**: transparently decompresses any response where `Content-Encoding: zstd` is set.
- **Accept-Encoding**: adds `zstd` to the advertised encodings.

Pass `nil` or an empty slice for standard zstd without a dictionary.

### `NewZstdDictionaryCompressor`

```go
func NewZstdDictionaryCompressor(dict []byte) (*ZstdDictCompressor, error)
```

Creates a standalone compressor/decompressor. Safe for concurrent use.

### `ZstdDictCompressor.Compress`

```go
func (z *ZstdDictCompressor) Compress(data []byte) ([]byte, error)
```

Compresses `data` using the pre-trained dictionary (if any).

### `ZstdDictCompressor.Decompress`

```go
func (z *ZstdDictCompressor) Decompress(data []byte) ([]byte, error)
```

Decompresses `data` that was compressed with the matching dictionary.

### `ZstdDictCompressor.Encoding`

```go
func (z *ZstdDictCompressor) Encoding() string // returns "zstd"
```

---

## Notes

- **Both client and server must use the same dictionary.** The dictionary is part of the zstd frame and must be present on both sides for decompression to succeed.
- `ZstdDictCompressor` is safe for concurrent use; the underlying encoder and decoder are created once and reused.
- If the server does not return `Content-Encoding: zstd`, the response is passed through unmodified.
- For maximum benefit, train dictionaries on payloads that share structure (same JSON keys, similar value lengths). Dictionaries provide little benefit for already-compressed formats (images, video, pre-compressed archives).
