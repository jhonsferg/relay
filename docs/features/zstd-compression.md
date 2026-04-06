# Zstandard (zstd) Compression

relay supports transparent **Zstandard (zstd)** compression for both response
decompression and outgoing request body compression, alongside existing gzip,
deflate, and Brotli support.

## Response decompression

### Auto-negotiation (recommended)

Use `WithCompression(CompressionAuto)` to advertise all supported encodings and
automatically decompress responses:

```go
import "github.com/jhonsferg/relay"

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relay.WithCompression(relay.CompressionAuto),
)
```

The client sends:

```
Accept-Encoding: zstd, br, gzip, deflate
```

Responses with any of those `Content-Encoding` values are decompressed
transparently before the body is returned to your code.

### Specific algorithm

To advertise and decompress only one encoding, pass the matching constant:

```go
// zstd only
client := relay.New(relay.WithCompression(relay.CompressionZstd))

// Brotli only
client := relay.New(relay.WithCompression(relay.CompressionBrotli))

// gzip only
client := relay.New(relay.WithCompression(relay.CompressionGzip))
```

## Request body compression

Compress outgoing request bodies above a size threshold with
`WithRequestCompression`:

```go
client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    // compress request bodies larger than 512 bytes with zstd
    relay.WithRequestCompression(relay.CompressionZstd, 512),
)

resp, err := client.Execute(
    client.Post("/ingest").WithBody(largePayload),
)
```

The middleware reads the body, compresses it when `len(body) >= minBytes`, and
sets the appropriate `Content-Encoding` header. Pass `minBytes <= 0` to use the
default threshold of 1024 bytes.

| Constant              | `Content-Encoding` sent |
|-----------------------|-------------------------|
| `CompressionZstd`     | `zstd`                  |
| `CompressionAuto`     | `zstd`                  |
| `CompressionBrotli`   | `br`                    |
| `CompressionGzip`     | `gzip`                  |

## Combining response and request compression

```go
client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relay.WithCompression(relay.CompressionAuto),
    relay.WithRequestCompression(relay.CompressionZstd, 1024),
)
```

## API reference

| Function | Description |
|---|---|
| `WithCompression(algo CompressionAlgorithm) Option` | Transparent response decompression with Accept-Encoding negotiation |
| `WithRequestCompression(algo CompressionAlgorithm, minBytes int) Option` | Compress outgoing request bodies above the size threshold |
| `CompressionAuto` | All encodings (zstd, br, gzip, deflate) |
| `CompressionZstd` | Zstandard only |
| `CompressionBrotli` | Brotli only |
| `CompressionGzip` | gzip only |

## Backward compatibility

The existing `WithDisableCompression()` option and the
`github.com/jhonsferg/relay/ext/brotli` extension continue to work unchanged.
`WithCompression` and `WithRequestCompression` operate as independent transport
middlewares and can be combined freely with other relay options.
