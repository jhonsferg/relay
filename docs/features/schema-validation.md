# Schema Validation

relay can validate decoded response bodies against constraints before returning them to your code. Two built-in validators are provided: `StructValidator` (uses Go struct tags) and `JSONSchemaValidator` (a subset of JSON Schema). You can also implement the `SchemaValidator` interface to plug in any custom logic.

---

## Usage

### StructValidator

Validate a response by defining a Go struct with `validate` tags:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

type UserResponse struct {
    ID    int    `json:"id"    validate:"required,min=1"`
    Name  string `json:"name"  validate:"required,min=1,max=100"`
    Email string `json:"email" validate:"required,min=5"`
}

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithSchemaValidator(relay.NewStructValidator(UserResponse{})),
    )
    if err != nil {
        log.Fatal(err)
    }

    var user UserResponse
    _, err = relay.Get[UserResponse](context.Background(), client, "/users/1", nil)
    if err != nil {
        // err is a *relay.ValidationError when constraints fail
        log.Fatal(err)
    }
    fmt.Printf("user: %+v\n", user)
}
```

### JSONSchemaValidator

Validate against a JSON Schema (subset):

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

const schema = `{
  "type": "object",
  "required": ["id", "name"],
  "properties": {
    "id":    { "type": "integer", "minimum": 1 },
    "name":  { "type": "string",  "minLength": 1, "maxLength": 100 },
    "email": { "type": "string",  "pattern": "^[^@]+@[^@]+$" }
  }
}`

func main() {
    validator, err := relay.NewJSONSchemaValidator(schema)
    if err != nil {
        log.Fatal("invalid schema:", err)
    }

    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithSchemaValidator(validator),
    )
    if err != nil {
        log.Fatal(err)
    }

    _, err = relay.Get[map[string]any](context.Background(), client, "/users/1", nil)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("response passed schema validation")
}
```

---

## API Reference

### `SchemaValidator` Interface

```go
type SchemaValidator interface {
    Validate(v interface{}) error
}
```

Implement this interface to provide custom validation. `v` is the result of `json.Unmarshal` into `interface{}`  -  an `map[string]interface{}` for JSON objects.

### `WithSchemaValidator`

```go
func WithSchemaValidator(v SchemaValidator) Option
```

Attaches a validator to the client. Called after JSON decoding for every response.

### `NewStructValidator`

```go
func NewStructValidator(prototype interface{}) *StructValidator
```

Creates a validator from a struct prototype. `prototype` may be a value or a pointer to a struct.

**Supported `validate` tags:**

| Tag | Applies to | Description |
|-----|-----------|-------------|
| `required` | any | Field must be non-zero |
| `min=N` | string, number | String: min length; Number: min value |
| `max=N` | string, number | String: max length; Number: max value |

Multiple rules are comma-separated: `validate:"required,min=1,max=255"`.

### `NewJSONSchemaValidator`

```go
func NewJSONSchemaValidator(schemaJSON string) (*JSONSchemaValidator, error)
```

Parses a JSON Schema string and returns a validator. Returns an error if the JSON is invalid.

**Supported JSON Schema keywords:**

| Keyword | Scope | Description |
|---------|-------|-------------|
| `type` | any | Type check: `object`, `array`, `string`, `number`, `integer`, `boolean`, `null` |
| `required` | object | List of required property names |
| `properties` | object | Per-property sub-schemas (recursive) |
| `minLength` / `maxLength` | string | Length constraints |
| `minimum` / `maximum` | number | Value constraints |
| `pattern` | string | Regular expression match |

### `ValidationError`

```go
type ValidationError struct {
    Field   string
    Message string
}
```

Returned when validation fails. `Field` is the JSON field path; `Message` describes the failure.

---

## Notes

- `StructValidator` re-encodes the decoded value back to JSON and then unmarshals it into the prototype, so field names follow `json:` tags.
- `JSONSchemaValidator` only implements a subset of the JSON Schema specification. Complex keywords (`allOf`, `anyOf`, `$ref`, `enum`, etc.) are not supported.
- Validators run after successful HTTP responses (2xx); they do not run on HTTP error responses.
