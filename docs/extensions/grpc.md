# gRPC Bridge Extension

The gRPC Bridge extension lets you call gRPC services through the relay HTTP client using HTTP/JSON-to-gRPC transcoding. Instead of managing raw gRPC connections directly, you describe your proto service with a `protodesc.File` and relay handles serialization, header mapping, and error translation automatically.

**Import path:** `github.com/jhonsferg/relay/ext/grpc`

---

## Overview

gRPC services are traditionally accessed over HTTP/2 with Protobuf binary framing. The bridge extension intercepts outgoing relay requests, re-encodes them as gRPC calls using proto descriptors, and returns responses as decoded JSON. This approach lets you reuse relay middleware (retry, circuit breaking, observability) for gRPC backends without a separate gRPC client stack.

Key capabilities:
- HTTP/JSON to gRPC transcoding via `google.api.http` annotations or direct method mapping
- Proto descriptor-driven encoding and decoding
- Automatic `grpc-status` to HTTP status code mapping
- Unary and server-streaming support
- Full relay middleware compatibility

---

## Installation

```bash
go get github.com/jhonsferg/relay/ext/grpc@latest
```

This extension depends on:
- `google.golang.org/grpc`
- `google.golang.org/protobuf`
- `github.com/jhonsferg/relay`

---

## Options

### `relaygrpc.WithGRPCTarget`

```go
relaygrpc.WithGRPCTarget(target string) relay.Option
```

Sets the gRPC server address. Accepts any address supported by the gRPC dial target syntax:
- `"dns:///greet.example.com:443"` - DNS resolution with optional load balancing
- `"localhost:50051"` - direct address
- `"passthrough:///10.0.1.5:443"` - bypass resolver

> **Note:** TLS is enabled by default when the port is 443. Use `relaygrpc.WithInsecure()` for plaintext connections during local development.

### `relaygrpc.WithProtoDescriptor`

```go
relaygrpc.WithProtoDescriptor(desc protodesc.File) relay.Option
```

Provides the compiled proto file descriptor that describes the gRPC service. The extension uses this descriptor to locate the correct RPC method, encode the JSON request body into a Protobuf binary message, and decode the Protobuf response back to JSON.

You obtain a `protodesc.File` from your generated `*_grpc.pb.go` file via `protodesc.ToFileDescriptorProto` or by embedding the raw descriptor bytes.

### `relaygrpc.WithInsecure`

```go
relaygrpc.WithInsecure() relay.Option
```

Disables TLS for the gRPC connection. Use this only for local development or internal services within a trusted network.

### `relaygrpc.WithGRPCDialOption`

```go
relaygrpc.WithGRPCDialOption(opt grpc.DialOption) relay.Option
```

Passes a raw `grpc.DialOption` to the underlying gRPC connection. Use this for custom credentials, interceptors, or keepalive configuration.

---

## Basic Usage

### Proto Definition

Assume you have the following proto file defining a simple greeting service:

```protobuf
syntax = "proto3";
package greet.v1;
option go_package = "example.com/gen/greet/v1;greetv1";

import "google/api/annotations.proto";

service GreetService {
  rpc SayHello (HelloRequest) returns (HelloResponse) {
    option (google.api.http) = {
      post: "/v1/greet"
      body: "*"
    };
  }
  rpc SayHelloStream (HelloRequest) returns (stream HelloResponse) {
    option (google.api.http) = {
      get: "/v1/greet/stream"
    };
  }
}

message HelloRequest {
  string name    = 1;
  string locale  = 2;
}

message HelloResponse {
  string greeting = 1;
  int64  timestamp = 2;
}
```

### Unary Call Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    relay "github.com/jhonsferg/relay"
    relaygrpc "github.com/jhonsferg/relay/ext/grpc"
    greetv1 "example.com/gen/greet/v1"
    "google.golang.org/protobuf/reflect/protodesc"
)

func main() {
    // Obtain the file descriptor from the generated package.
    // greetv1.File_greet_v1_greet_proto is the protoreflect.FileDescriptor
    // embedded by protoc-gen-go in the generated _grpc.pb.go file.
    fd, err := protodesc.NewFile(
        greetv1.File_greet_v1_greet_proto.ParentFile().ParentFile().
            // walk up to the raw FileDescriptorProto
            // In practice you call the generated File_ variable directly:
            ParentFile().Options().ProtoReflect().Descriptor().ParentFile(),
        nil,
    )
    if err != nil {
        log.Fatalf("descriptor: %v", err)
    }

    client, err := relay.NewClient(
        relaygrpc.WithGRPCTarget("dns:///greet.example.com:443"),
        relaygrpc.WithProtoDescriptor(fd),
        relay.WithTimeout(10*time.Second),
    )
    if err != nil {
        log.Fatalf("relay client: %v", err)
    }

    type HelloRequest struct {
        Name   string `json:"name"`
        Locale string `json:"locale"`
    }
    type HelloResponse struct {
        Greeting  string `json:"greeting"`
        Timestamp int64  `json:"timestamp"`
    }

    var resp HelloResponse
    err = client.Post(context.Background(), "/v1/greet", &HelloRequest{
        Name:   "Alice",
        Locale: "en-US",
    }, &resp)
    if err != nil {
        log.Fatalf("rpc: %v", err)
    }

    fmt.Printf("Got: %s (at %d)\n", resp.Greeting, resp.Timestamp)
}
```

> **Tip:** If you use `google.api.http` annotations in your proto file, the extension automatically maps incoming relay request paths and methods to the correct RPC without any extra configuration.

---

## Error Mapping

gRPC defines a set of status codes in the `google.golang.org/grpc/codes` package. The bridge extension maps these to HTTP status codes so your relay error handlers receive familiar HTTP errors.

| gRPC Status Code      | HTTP Status |
|-----------------------|-------------|
| `OK`                  | 200         |
| `INVALID_ARGUMENT`    | 400         |
| `NOT_FOUND`           | 404         |
| `ALREADY_EXISTS`      | 409         |
| `PERMISSION_DENIED`   | 403         |
| `UNAUTHENTICATED`     | 401         |
| `RESOURCE_EXHAUSTED`  | 429         |
| `FAILED_PRECONDITION` | 400         |
| `UNIMPLEMENTED`       | 501         |
| `UNAVAILABLE`         | 503         |
| `DEADLINE_EXCEEDED`   | 504         |
| `INTERNAL`            | 500         |
| `UNKNOWN`             | 500         |

The full gRPC error detail (including `google.rpc.Status` message and any `Details` entries) is preserved inside the relay error value so you can inspect it:

```go
import (
    "errors"
    relaygrpc "github.com/jhonsferg/relay/ext/grpc"
    "google.golang.org/grpc/status"
)

var resp HelloResponse
err := client.Post(ctx, "/v1/greet", req, &resp)
if err != nil {
    var grpcErr *relaygrpc.StatusError
    if errors.As(err, &grpcErr) {
        st := grpcErr.GRPCStatus()
        fmt.Printf("gRPC code=%s msg=%s\n", st.Code(), st.Message())
        for _, detail := range st.Details() {
            fmt.Printf("  detail: %T %v\n", detail, detail)
        }
    }
    return err
}
```

---

## Metadata and Headers

gRPC metadata maps directly to HTTP headers. Outgoing relay request headers that are prefixed with `grpc-` or `x-` are forwarded as gRPC metadata:

```go
import "net/http"

ctx := context.Background()
req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/v1/greet", nil)
req.Header.Set("x-request-id", "abc-123")
req.Header.Set("authorization", "Bearer token-xyz")

// relay will forward these as gRPC metadata entries:
// - x-request-id: abc-123
// - authorization: Bearer token-xyz
```

Incoming gRPC response metadata is translated back into HTTP response headers. This includes `grpc-status`, `grpc-message`, and any application-defined trailing metadata.

---

## Streaming Considerations

Server-streaming RPCs return a sequence of messages. The extension buffers the full stream into a JSON array before returning the relay response. This is convenient for small streams but unsuitable for large or long-lived streams.

```go
type HelloResponse struct {
    Greeting  string `json:"greeting"`
    Timestamp int64  `json:"timestamp"`
}

// The bridge returns a JSON array: [{"greeting":"..."},{"greeting":"..."}]
var responses []HelloResponse
err := client.Get(ctx, "/v1/greet/stream?name=Alice", &responses)
if err != nil {
    log.Fatal(err)
}
for _, r := range responses {
    fmt.Println(r.Greeting)
}
```

> **Warning:** Do not use the buffered streaming mode for RPCs that produce unbounded or very large streams. For those cases, use `relaygrpc.StreamHandler` directly, which provides a callback-based API that processes messages one at a time without buffering the full stream in memory.

### Streaming with a Callback

```go
err := relaygrpc.Stream(ctx, client, "/v1/greet/stream", &HelloRequest{Name: "Bob"},
    func(msg *HelloResponse) error {
        fmt.Printf("received: %s\n", msg.Greeting)
        return nil
    },
)
if err != nil {
    log.Fatal(err)
}
```

Client-streaming and bidirectional streaming RPCs are not currently supported by the bridge extension. They require a persistent connection that HTTP/1.1 semantics cannot express.

---

## Complete End-to-End Example

The following example shows a production-style setup with TLS, retry, and deadline:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    relay "github.com/jhonsferg/relay"
    relaygrpc "github.com/jhonsferg/relay/ext/grpc"
    greetv1 "example.com/gen/greet/v1"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials"
    "google.golang.org/grpc/keepalive"
    "google.golang.org/protobuf/reflect/protodesc"
    "google.golang.org/protobuf/reflect/protoregistry"
)

func main() {
    // Build a file descriptor from the global registry.
    fd, err := protoregistry.GlobalFiles.FindFileByPath("greet/v1/greet.proto")
    if err != nil {
        log.Fatalf("find file: %v", err)
    }
    pdFile, err := protodesc.NewFile(
        protodesc.ToFileDescriptorProto(fd), nil,
    )
    if err != nil {
        log.Fatalf("new file: %v", err)
    }

    tlsCreds := credentials.NewClientTLSFromCert(nil, "")

    client, err := relay.NewClient(
        relaygrpc.WithGRPCTarget("dns:///greet.prod.example.com:443"),
        relaygrpc.WithProtoDescriptor(pdFile),
        relaygrpc.WithGRPCDialOption(grpc.WithTransportCredentials(tlsCreds)),
        relaygrpc.WithGRPCDialOption(grpc.WithKeepaliveParams(keepalive.ClientParameters{
            Time:                20 * time.Second,
            Timeout:             5 * time.Second,
            PermitWithoutStream: true,
        })),
        relay.WithTimeout(30*time.Second),
        relay.WithRetry(relay.RetryConfig{
            MaxAttempts: 3,
            WaitBase:    200 * time.Millisecond,
            // Only retry on UNAVAILABLE and DEADLINE_EXCEEDED gRPC codes,
            // which map to HTTP 503 and 504 respectively.
            RetryableStatus: []int{503, 504},
        }),
    )
    if err != nil {
        log.Fatalf("relay client: %v", err)
    }

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    type HelloRequest struct {
        Name   string `json:"name"`
        Locale string `json:"locale"`
    }
    type HelloResponse struct {
        Greeting  string `json:"greeting"`
        Timestamp int64  `json:"timestamp"`
    }

    var resp HelloResponse
    if err := client.Post(ctx, "/v1/greet", &HelloRequest{
        Name:   "World",
        Locale: "en-US",
    }, &resp); err != nil {
        log.Fatalf("hello: %v", err)
    }

    fmt.Printf("Response: %s\n", resp.Greeting)
    fmt.Printf("Server time: %s\n",
        time.Unix(resp.Timestamp, 0).UTC().Format(time.RFC3339))
}
```

---

## Testing with the Mock Transport

Combine the gRPC bridge with `relay/ext/mock` to write unit tests without a real gRPC server:

```go
package greet_test

import (
    "context"
    "encoding/json"
    "net/http"
    "strings"
    "testing"

    relay "github.com/jhonsferg/relay"
    relaygrpc "github.com/jhonsferg/relay/ext/grpc"
    relaymock "github.com/jhonsferg/relay/ext/mock"
)

func TestSayHello(t *testing.T) {
    transport := relaymock.NewTransport()

    body, _ := json.Marshal(map[string]any{
        "greeting":  "Hello, Alice!",
        "timestamp": 1700000000,
    })
    transport.Enqueue(&http.Response{
        StatusCode: http.StatusOK,
        Header:     http.Header{"Content-Type": []string{"application/json"}},
        Body:       http.NoBody,
    })
    // Override Body after construction to avoid nil dereference.
    transport.EnqueueFunc(func(req *http.Request) (*http.Response, error) {
        return &http.Response{
            StatusCode: http.StatusOK,
            Header:     http.Header{"Content-Type": []string{"application/json"}},
            Body:       io.NopCloser(strings.NewReader(string(body))),
        }, nil
    })

    client, _ := relay.NewClient(
        relaygrpc.WithGRPCTarget("localhost:50051"),
        relay.WithTransport(transport),
    )

    type Req struct{ Name string `json:"name"` }
    type Resp struct {
        Greeting  string `json:"greeting"`
        Timestamp int64  `json:"timestamp"`
    }

    var resp Resp
    if err := client.Post(context.Background(), "/v1/greet", &Req{Name: "Alice"}, &resp); err != nil {
        t.Fatal(err)
    }
    if resp.Greeting != "Hello, Alice!" {
        t.Errorf("unexpected greeting: %q", resp.Greeting)
    }
}
```

---

## See Also

- [Mock Transport Extension](mock.md) - unit testing without real servers
- [OAuth2 Extension](oauth.md) - adding authentication to gRPC requests
- relay core documentation - retry, circuit breaking, and observability
