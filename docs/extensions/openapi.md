# OpenAPI

The OpenAPI extension (`ext/openapi`) integrates relay with OpenAPI 3.x specifications, enabling auto-generated clients, request validation, and spec-driven testing.

## Installation

```bash
go get github.com/jhonsferg/relay/ext/openapi
```

## Loading a spec

```go
import (
    "github.com/jhonsferg/relay"
    "github.com/jhonsferg/relay/ext/openapi"
)

spec, err := openapi.LoadFile("api/openapi.yaml")
if err != nil {
    log.Fatal(err)
}

client := relay.New(relay.Config{
    BaseURL: spec.Servers[0].URL,
})

// Wrap client with OpenAPI middleware
oa := openapi.New(client, spec)
```

## Request validation

Validate outgoing requests against the spec before sending:

```go
oa := openapi.New(client, spec, openapi.Options{
    ValidateRequests: true,
})

var user User
_, err := oa.R().
    SetResult(&user).
    GET(ctx, "/users/123")
// Returns openapi.ErrValidation if path, query params, or body
// do not match the spec definition
```

## Response validation

Validate incoming responses:

```go
oa := openapi.New(client, spec, openapi.Options{
    ValidateRequests:  true,
    ValidateResponses: true,
    StrictMode:        false, // warn instead of error on unknown fields
})
```

## Typed operations

Generate a typed wrapper directly from the spec at build time:

```bash
go run github.com/jhonsferg/relay/ext/openapi/cmd/gen \
    --spec api/openapi.yaml \
    --out gen/client.go \
    --package gen
```

The generated client exposes typed methods per operation:

```go
import "myapp/gen"

c := gen.NewClient(relay.New(relay.Config{BaseURL: "https://api.example.com"}))

users, err := c.ListUsers(ctx, gen.ListUsersParams{
    Page:  1,
    Limit: 20,
})
```

## Spec-driven mock

Use the spec to auto-generate mock responses during tests:

```go
import "github.com/jhonsferg/relay/ext/openapi"

mockTransport := openapi.NewMockFromSpec(spec, openapi.MockOptions{
    UseExamples:  true,  // prefer spec examples
    GenerateFake: true,  // generate fake data when no example present
})

client := relay.New(relay.Config{Transport: mockTransport})
```

## Dynamic base URL selection

Select a server from the spec by environment name:

```go
spec, _ := openapi.LoadFile("openapi.yaml")

// spec.Servers may include prod, staging, sandbox URLs with x-environment tags
client := relay.New(relay.Config{
    BaseURL: spec.ServerByEnv("staging").URL,
})
```

## Validation error handling

```go
_, err := oa.R().POST(ctx, "/users", invalidPayload)
var ve *openapi.ValidationError
if errors.As(err, &ve) {
    for _, issue := range ve.Issues {
        log.Printf("field %q: %s", issue.Path, issue.Message)
    }
}
```

## See also

- [Mock Transport](mock.md)
- [Extensions Overview](index.md)
- [relay reference - Request Builder](../reference/request.md)
