# relay-gen: OpenAPI Code Generation

`relay-gen` is a CLI tool that reads an OpenAPI 3.x specification (JSON or YAML) and generates a type-safe Go client backed by the relay library. It produces two files: a `client.go` with one method per operation and a `models.go` with the request/response types derived from the spec's `components/schemas`.

---

## Installation

```bash
go install github.com/jhonsferg/relay/cmd/relay-gen@latest
```

---

## Usage

```bash
relay-gen -input openapi.json -output ./generated -package apiclient
```

---

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-input` | *(required)* | Path to the OpenAPI 3.x spec file (JSON or YAML) |
| `-output` | `./generated` | Output directory for generated files |
| `-package` | `client` | Go package name for generated code |
| `-module` | `github.com/jhonsferg/relay` | Module path used in the relay import |
| `-dry-run` | `false` | Print generated code to stdout instead of writing files |

---

## Complete Example

**1. Write a spec (`petstore.yaml`):**

```yaml
openapi: "3.0.3"
info:
  title: Petstore
  version: "1.0"
paths:
  /pets/{id}:
    get:
      operationId: getPet
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: integer
  /pets:
    post:
      operationId: createPet
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/NewPet"
components:
  schemas:
    NewPet:
      type: object
      required: [name]
      properties:
        name:
          type: string
        tag:
          type: string
```

**2. Generate the client:**

```bash
relay-gen \
  -input petstore.yaml \
  -output ./petstore \
  -package petstore
```

This writes `./petstore/client.go` and `./petstore/models.go`.

**3. Use the generated client:**

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
    "myapp/petstore"
)

func main() {
    base, err := relay.New(relay.WithBaseURL("https://api.example.com"))
    if err != nil {
        log.Fatal(err)
    }
    c := petstore.NewClient(base)

    // GET /pets/42
    resp, err := c.GetPet(context.Background(), 42)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("pet: %+v\n", resp)

    // POST /pets
    created, err := c.CreatePet(context.Background(), petstore.NewPet{Name: "Fido"})
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("created: %+v\n", created)
}
```

---

## Dry-run / Preview

Use `-dry-run` to preview the generated code without writing any files:

```bash
relay-gen -input petstore.yaml -dry-run
```

---

## Notes

- Only OpenAPI 3.x (JSON and YAML) is supported. OpenAPI 2.x (Swagger) specs are not supported.
- `operationId` is used as the method name (converted to PascalCase). Operations without an `operationId` are skipped with a warning.
- Path parameters (`in: path`) and query parameters (`in: query`) are reflected as Go function arguments. Header and cookie parameters are not yet generated.
- `$ref` references inside `requestBody` are resolved from `components/schemas`.
- The generated code depends on the relay module; run `go mod tidy` after generation.
