# Content Negotiation

Content negotiation is the mechanism by which a client and server agree on the format of data exchanged in an HTTP request or response. `relay` provides a flexible codec system that lets you register custom encoders and decoders for any `Content-Type`, swap out the default JSON codec, and control the `Accept` header sent with every request.

By default, `relay` encodes request bodies as JSON and decodes response bodies as JSON. This works for the vast majority of REST APIs. When you need to work with binary formats like Protocol Buffers or MessagePack - for performance, wire-size reduction, or compatibility with existing systems - the codec API makes it straightforward.

---

## WithContentTypeEncoder

```go
func WithContentTypeEncoder(contentType string, encoderFunc func(v interface{}) ([]byte, error)) Option
```

Registers a custom encoder for a specific `Content-Type`. When you call `client.Post`, `client.Put`, or `client.Patch` with a body value, `relay` uses the encoder registered for the client's default content type (or the one set in the request options) to marshal the value into bytes.

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
    // Register a custom JSON encoder that uses a non-default configuration:
    // HTML escaping is disabled and indentation is applied.
    customJSON := func(v interface{}) ([]byte, error) {
        buf := &bytes.Buffer{}
        enc := json.NewEncoder(buf)
        enc.SetEscapeHTML(false)
        enc.SetIndent("", "  ")
        if err := enc.Encode(v); err != nil {
            return nil, err
        }
        return buf.Bytes(), nil
    }

    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithContentTypeEncoder("application/json", customJSON),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Post(context.Background(), "/events", map[string]interface{}{
        "type":    "page_view",
        "url":     "https://example.com/products",
        "user_id": "u-12345",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```

> **note**
> The `contentType` string is matched against the `Content-Type` header being sent. It should be the MIME type without parameters (e.g., `"application/json"`, not `"application/json; charset=utf-8"`). `relay` strips parameters before lookup.

---

## WithContentTypeDecoder

```go
func WithContentTypeDecoder(contentType string, decoderFunc func(data []byte, v interface{}) error) Option
```

Registers a custom decoder for a specific `Content-Type`. When a response body arrives, `relay` inspects the `Content-Type` response header and calls the matching decoder to unmarshal the body into your target value.

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
    // Register a lenient JSON decoder that tolerates unknown fields.
    // (This is actually the default behavior for encoding/json,
    //  but the pattern shows how to install a custom decoder.)
    lenientJSON := func(data []byte, v interface{}) error {
        dec := json.NewDecoder(bytes.NewReader(data))
        dec.DisallowUnknownFields() // or remove this for lenient mode
        return dec.Decode(v)
    }

    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithContentTypeDecoder("application/json", lenientJSON),
    )
    if err != nil {
        log.Fatal(err)
    }

    var result struct {
        ID   string `json:"id"`
        Name string `json:"name"`
    }
    resp, err := client.Get(context.Background(), "/users/me", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    if err := relay.DecodeResponse(resp, &result); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("user: id=%s name=%s\n", result.ID, result.Name)
}
```

---

## WithDefaultAccept

```go
func WithDefaultAccept(mediaType string) Option
```

Sets the `Accept` header sent on every request that does not have an explicit `Accept` header override. This signals to the server which response format the client prefers.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    // Tell the server we prefer MessagePack, but will accept JSON as fallback.
    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithDefaultAccept("application/msgpack, application/json;q=0.9"),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/products/42", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("response content-type:", resp.Header.Get("Content-Type"))
}
```

---

## Default JSON Encoder/Decoder

Out of the box, `relay` registers the standard library `encoding/json` encoder and decoder for `application/json`. You do not need to configure anything to use JSON - it works automatically.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

type CreateUserRequest struct {
    Name  string `json:"name"`
    Email string `json:"email"`
    Role  string `json:"role"`
}

type CreateUserResponse struct {
    ID        string `json:"id"`
    CreatedAt string `json:"created_at"`
}

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://users.internal"),
    )
    if err != nil {
        log.Fatal(err)
    }

    // relay automatically encodes this as JSON with Content-Type: application/json
    req := CreateUserRequest{
        Name:  "Alice",
        Email: "alice@example.com",
        Role:  "admin",
    }

    resp, err := client.Post(context.Background(), "/users", req)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    var created CreateUserResponse
    if err := relay.DecodeResponse(resp, &created); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("created user: id=%s at=%s\n", created.ID, created.CreatedAt)
}
```

The default `Accept` header is `application/json`. If you register a custom decoder for another type, you should also update `WithDefaultAccept` to reflect that preference.

---

## Custom Encoder Example: Protobuf Encoding

Protocol Buffers (protobuf) produce significantly smaller payloads than JSON for structured data - typically 3x to 10x smaller. When working with internal microservices, protobuf is a common choice.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "google.golang.org/protobuf/proto"

    "github.com/jhonsferg/relay"
    // Assume this is your generated protobuf package
    // pb "github.com/yourorg/yourservice/proto"
)

const protoContentType = "application/x-protobuf"

func protoEncoder(v interface{}) ([]byte, error) {
    msg, ok := v.(proto.Message)
    if !ok {
        return nil, fmt.Errorf("relay: protobuf encoder: value %T does not implement proto.Message", v)
    }
    return proto.Marshal(msg)
}

func protoDecoder(data []byte, v interface{}) error {
    msg, ok := v.(proto.Message)
    if !ok {
        return fmt.Errorf("relay: protobuf decoder: value %T does not implement proto.Message", v)
    }
    return proto.Unmarshal(data, msg)
}

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://catalog.internal"),
        relay.WithContentTypeEncoder(protoContentType, protoEncoder),
        relay.WithContentTypeDecoder(protoContentType, protoDecoder),
        relay.WithDefaultAccept(protoContentType),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Assume pb.GetItemRequest is a generated proto message
    // reqMsg := &pb.GetItemRequest{ItemId: "item-42"}
    // resp, err := client.Get(ctx, "/items/42", nil)
    // if err != nil { log.Fatal(err) }
    // defer resp.Body.Close()
    // var result pb.Item
    // relay.DecodeResponse(resp, &result)
    // fmt.Println("item name:", result.Name)

    // Placeholder to keep the example compilable without the proto package:
    resp, err := client.Get(context.Background(), "/items/42", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```

> **tip**
> When using protobuf with `relay`, always set `WithDefaultAccept("application/x-protobuf")` so the server knows you prefer binary responses. Many servers that support both JSON and protobuf use the `Accept` header to decide which format to return.

---

## Custom Decoder Example: MessagePack Decoding

MessagePack is a binary serialization format that is faster to encode/decode than JSON and produces smaller payloads. It is particularly popular in high-throughput internal APIs.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/vmihailenco/msgpack/v5"

    "github.com/jhonsferg/relay"
)

const msgpackContentType = "application/msgpack"

func msgpackEncoder(v interface{}) ([]byte, error) {
    return msgpack.Marshal(v)
}

func msgpackDecoder(data []byte, v interface{}) error {
    return msgpack.Unmarshal(data, v)
}

type Product struct {
    ID    string  `msgpack:"id"`
    Name  string  `msgpack:"name"`
    Price float64 `msgpack:"price"`
    Stock int     `msgpack:"stock"`
}

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://products.internal"),
        relay.WithContentTypeEncoder(msgpackContentType, msgpackEncoder),
        relay.WithContentTypeDecoder(msgpackContentType, msgpackDecoder),
        relay.WithDefaultAccept(msgpackContentType+", application/json;q=0.8"),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/products/99", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    var product Product
    if err := relay.DecodeResponse(resp, &product); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("product: id=%s name=%s price=%.2f stock=%d\n",
        product.ID, product.Name, product.Price, product.Stock)
}
```

---

## Content-Type and Accept Header Interaction

The `Content-Type` and `Accept` headers serve different purposes:

- `Content-Type` describes the format of the **request body** you are sending to the server.
- `Accept` describes the formats you are willing to receive in the **response body**.

`relay` sets `Content-Type` automatically based on the encoder used for the outgoing body. `Accept` is set from `WithDefaultAccept` unless overridden per-request.

```go
package main

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "log"
    "net/http"

    "github.com/jhonsferg/relay"
)

// contentNegotiationMiddleware logs the negotiation headers on each request.
func contentNegotiationMiddleware(next http.RoundTripper) http.RoundTripper {
    return relay.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
        fmt.Printf("  --> Content-Type: %s\n", req.Header.Get("Content-Type"))
        fmt.Printf("  --> Accept:       %s\n", req.Header.Get("Accept"))
        resp, err := next.RoundTrip(req)
        if err != nil {
            return nil, err
        }
        fmt.Printf("  <-- Content-Type: %s\n", resp.Header.Get("Content-Type"))
        return resp, nil
    })
}

func main() {
    // Client sends JSON bodies and accepts either JSON or YAML responses
    client, err := relay.New(
        relay.WithBaseURL("https://config.internal"),
        relay.WithDefaultAccept("application/json, application/yaml;q=0.9"),
        relay.WithTransportMiddleware(contentNegotiationMiddleware),
        // Custom YAML decoder (hypothetical)
        relay.WithContentTypeDecoder("application/yaml", func(data []byte, v interface{}) error {
            // yaml.Unmarshal(data, v)
            return json.Unmarshal(data, v) // fallback for this example
        }),
    )
    if err != nil {
        log.Fatal(err)
    }

    // POST with JSON body - Content-Type is set to application/json automatically
    body := map[string]interface{}{
        "key":   "feature_flags",
        "value": map[string]bool{"dark_mode": true},
    }
    resp, err := client.Post(context.Background(), "/config", body)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    fmt.Println("response status:", resp.StatusCode)

    // GET - no body, only Accept header matters
    resp2, err := client.Get(context.Background(), "/config/feature_flags", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp2.Body.Close()

    _ = bytes.NewReader // suppress unused import
    fmt.Println("response status:", resp2.StatusCode)
}
```

> **note**
> When you call `client.Get()` with no body, `relay` does not set a `Content-Type` header (there is no body to describe). The `Accept` header is always set based on `WithDefaultAccept` unless overridden.

---

## Registering Multiple Codecs

A single client can support multiple content types simultaneously. `relay` selects the right codec based on the response's `Content-Type` header.

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"

    "github.com/vmihailenco/msgpack/v5"
    "google.golang.org/protobuf/proto"

    "github.com/jhonsferg/relay"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://api.internal"),

        // JSON (also the default, but shown here for clarity)
        relay.WithContentTypeEncoder("application/json", json.Marshal),
        relay.WithContentTypeDecoder("application/json", json.Unmarshal),

        // MessagePack
        relay.WithContentTypeEncoder("application/msgpack", msgpack.Marshal),
        relay.WithContentTypeDecoder("application/msgpack", msgpack.Unmarshal),

        // Protocol Buffers
        relay.WithContentTypeEncoder("application/x-protobuf", func(v interface{}) ([]byte, error) {
            if msg, ok := v.(proto.Message); ok {
                return proto.Marshal(msg)
            }
            return nil, fmt.Errorf("not a proto.Message")
        }),
        relay.WithContentTypeDecoder("application/x-protobuf", func(data []byte, v interface{}) error {
            if msg, ok := v.(proto.Message); ok {
                return proto.Unmarshal(data, msg)
            }
            return fmt.Errorf("not a proto.Message")
        }),

        // Client prefers msgpack, falls back to protobuf, then JSON
        relay.WithDefaultAccept("application/msgpack, application/x-protobuf;q=0.9, application/json;q=0.8"),
    )
    if err != nil {
        log.Fatal(err)
    }

    log.Println("multi-codec client ready:", client)
}
```

---

## Summary

| Feature | API |
|---|---|
| Register request body encoder | `WithContentTypeEncoder(contentType, fn)` |
| Register response body decoder | `WithContentTypeDecoder(contentType, fn)` |
| Set preferred response format | `WithDefaultAccept(mediaType)` |
| Default behavior | JSON encode/decode, `Accept: application/json` |
| Binary format (smaller payloads) | Register protobuf or msgpack codecs |
| Multiple formats on one client | Register multiple encoders/decoders |
