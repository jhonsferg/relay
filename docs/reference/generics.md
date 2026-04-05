# Generic Response Decoders

relay provides a set of generic helper functions for decoding `*relay.Response` bodies into typed Go values. These functions eliminate the boilerplate of reading bytes, checking content types, and calling `json.Unmarshal` or `xml.Unmarshal` manually. All decoder functions automatically close the response body after reading.

---

## Overview

Before generics were added in Go 1.18, decoding a JSON response required:

```go
// Without generics (verbose)
body, err := io.ReadAll(resp.Body)
resp.Body.Close()
if err != nil {
    return err
}
var user User
if err := json.Unmarshal(body, &user); err != nil {
    return err
}
```

With relay's generic decoders:

```go
// With generics (concise)
user, err := relay.DecodeJSON[User](resp)
```

The type parameter `T` can be any Go type: struct, slice, map, scalar, or interface.

---

## relay.DecodeJSON

```go
func DecodeJSON[T any](resp *relay.Response) (T, error)
```

`DecodeJSON` reads the response body and decodes it as JSON into a value of type `T`. The body is closed after reading regardless of whether decoding succeeds.

### Type Parameters

| Parameter | Constraint | Description |
|-----------|------------|-------------|
| `T` | `any` | The target type. Must be JSON-decodable. |

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `resp` | `*relay.Response` | The response to decode. Must have a non-nil, unread body. |

### Return Values

| Value | Description |
|-------|-------------|
| `T` | The decoded value. Zero value of `T` on error. |
| `error` | Non-nil if reading the body or JSON decoding fails. |

### Example - Decoding a struct

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

type User struct {
    ID       string `json:"id"`
    Name     string `json:"name"`
    Email    string `json:"email"`
    Role     string `json:"role"`
    Verified bool   `json:"verified"`
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    resp, err := client.Execute(context.Background(), client.Get("/users/u-42"))
    if err != nil {
        log.Fatal(err)
    }

    user, err := relay.DecodeJSON[User](resp)
    if err != nil {
        log.Fatal("decode error:", err)
    }

    fmt.Printf("user: %s (%s) role=%s verified=%v\n",
        user.Name, user.Email, user.Role, user.Verified)
}
```

### Example - Decoding a slice

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

type Product struct {
    ID    string  `json:"id"`
    Name  string  `json:"name"`
    Price float64 `json:"price"`
    Tags  []string `json:"tags"`
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    resp, err := client.Execute(
        context.Background(),
        client.Get("/products").WithQueryParam("category", "electronics"),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Decode directly into []Product
    products, err := relay.DecodeJSON[[]Product](resp)
    if err != nil {
        log.Fatal("decode error:", err)
    }

    fmt.Printf("found %d products:\n", len(products))
    for _, p := range products {
        fmt.Printf("  - %s ($%.2f) tags=%v\n", p.Name, p.Price, p.Tags)
    }
}
```

### Example - Decoding a map

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    resp, err := client.Execute(context.Background(), client.Get("/config/feature-flags"))
    if err != nil {
        log.Fatal(err)
    }

    // Decode into a map for dynamic/unknown keys
    flags, err := relay.DecodeJSON[map[string]bool](resp)
    if err != nil {
        log.Fatal("decode error:", err)
    }

    for flag, enabled := range flags {
        fmt.Printf("  %s: %v\n", flag, enabled)
    }
}
```

### Example - Decoding nested maps (arbitrary JSON)

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    resp, err := client.Execute(context.Background(), client.Get("/debug/state"))
    if err != nil {
        log.Fatal(err)
    }

    // Use map[string]interface{} for fully dynamic JSON
    state, err := relay.DecodeJSON[map[string]interface{}](resp)
    if err != nil {
        log.Fatal("decode error:", err)
    }

    if version, ok := state["version"].(string); ok {
        fmt.Println("server version:", version)
    }
    if uptime, ok := state["uptime_seconds"].(float64); ok {
        fmt.Printf("uptime: %.0f seconds\n", uptime)
    }
}
```

---

## relay.DecodeXML

```go
func DecodeXML[T any](resp *relay.Response) (T, error)
```

`DecodeXML` reads the response body and decodes it as XML into a value of type `T` using the standard `encoding/xml` package. The body is closed after reading.

### Type Parameters

| Parameter | Constraint | Description |
|-----------|------------|-------------|
| `T` | `any` | The target type. Must have appropriate `xml:` struct tags or implement `xml.Unmarshaler`. |

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `resp` | `*relay.Response` | The response to decode. |

### Return Values

| Value | Description |
|-------|-------------|
| `T` | The decoded value. Zero value of `T` on error. |
| `error` | Non-nil if reading or XML decoding fails. |

### Example - Decoding an XML struct

```go
package main

import (
    "context"
    "encoding/xml"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

type Order struct {
    XMLName    xml.Name `xml:"Order"`
    ID         string   `xml:"Id"`
    CustomerID string   `xml:"CustomerId"`
    Status     string   `xml:"Status"`
    Items      []struct {
        SKU      string  `xml:"SKU"`
        Quantity int     `xml:"Quantity"`
        Price    float64 `xml:"Price"`
    } `xml:"Items>Item"`
}

func main() {
    client := relay.New(relay.WithBaseURL("https://legacy-api.example.com"))

    resp, err := client.Execute(
        context.Background(),
        client.Get("/orders/ord-12345").
            WithHeader("Accept", "application/xml"),
    )
    if err != nil {
        log.Fatal(err)
    }

    order, err := relay.DecodeXML[Order](resp)
    if err != nil {
        log.Fatal("XML decode error:", err)
    }

    fmt.Printf("order %s for customer %s: status=%s\n",
        order.ID, order.CustomerID, order.Status)
    for _, item := range order.Items {
        fmt.Printf("  SKU: %s x%d @ $%.2f\n", item.SKU, item.Quantity, item.Price)
    }
}
```

### Example - Decoding XML list

```go
package main

import (
    "context"
    "encoding/xml"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

type ProductList struct {
    XMLName  xml.Name `xml:"Products"`
    Products []struct {
        ID   string `xml:"id,attr"`
        Name string `xml:"Name"`
    } `xml:"Product"`
}

func main() {
    client := relay.New(relay.WithBaseURL("https://catalog.example.com"))

    resp, err := client.Execute(context.Background(), client.Get("/products.xml"))
    if err != nil {
        log.Fatal(err)
    }

    list, err := relay.DecodeXML[ProductList](resp)
    if err != nil {
        log.Fatal("decode error:", err)
    }

    fmt.Printf("found %d products:\n", len(list.Products))
    for _, p := range list.Products {
        fmt.Printf("  [%s] %s\n", p.ID, p.Name)
    }
}
```

---

## relay.DecodeAs

```go
func DecodeAs[T any](resp *relay.Response) (T, error)
```

`DecodeAs` automatically selects the appropriate decoder based on the response `Content-Type` header. This is the most ergonomic choice when working with APIs that may return multiple content types, or when you want to future-proof your decoder usage.

### Content-Type Dispatch Rules

| Content-Type | Decoder Used |
|-------------|--------------|
| `application/json` | `encoding/json` |
| `text/json` | `encoding/json` |
| `application/xml` | `encoding/xml` |
| `text/xml` | `encoding/xml` |
| Custom types registered via `WithContentTypeDecoder` | Your custom decoder |
| Unknown | Returns error |

### Type Parameters

| Parameter | Constraint | Description |
|-----------|------------|-------------|
| `T` | `any` | The target type. Must be compatible with the actual Content-Type decoder. |

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `resp` | `*relay.Response` | The response whose Content-Type determines the decoder. |

### Return Values

| Value | Description |
|-------|-------------|
| `T` | The decoded value. Zero value of `T` on error. |
| `error` | Non-nil if the Content-Type is unsupported, or decoding fails. |

### Example - Content-negotiated endpoint

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

type Report struct {
    Title   string `json:"title"  xml:"Title"`
    Period  string `json:"period" xml:"Period"`
    Revenue int64  `json:"revenue" xml:"Revenue"`
}

func fetchReport(client *relay.Client, format string) (*Report, error) {
    resp, err := client.Execute(
        context.Background(),
        client.Get("/reports/monthly").
            WithHeader("Accept", format),
    )
    if err != nil {
        return nil, err
    }

    report, err := relay.DecodeAs[Report](resp)
    if err != nil {
        return nil, fmt.Errorf("decode (%s) failed: %w", resp.ContentType(), err)
    }
    return &report, nil
}

func main() {
    client := relay.New(relay.WithBaseURL("https://reports.example.com"))

    // Fetch as JSON
    jsonReport, err := fetchReport(client, "application/json")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("JSON: %s - revenue: %d\n", jsonReport.Title, jsonReport.Revenue)

    // Fetch as XML - same struct, same decode call
    xmlReport, err := fetchReport(client, "application/xml")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("XML: %s - revenue: %d\n", xmlReport.Title, xmlReport.Revenue)
}
```

### Example - With custom MessagePack decoder

```go
package main

import (
    "context"
    "fmt"
    "log"

    msgpack "github.com/vmihailenco/msgpack/v5"
    "github.com/jhonsferg/relay"
)

type Metric struct {
    Name  string  `msgpack:"name"`
    Value float64 `msgpack:"value"`
    Tags  []string `msgpack:"tags"`
}

func main() {
    client := relay.New(
        relay.WithBaseURL("https://metrics.example.com"),
        relay.WithContentTypeDecoder("application/msgpack", func(data []byte, v interface{}) error {
            return msgpack.Unmarshal(data, v)
        }),
    )

    resp, err := client.Execute(
        context.Background(),
        client.Get("/metrics/cpu").WithHeader("Accept", "application/msgpack"),
    )
    if err != nil {
        log.Fatal(err)
    }

    // DecodeAs picks the msgpack decoder based on Content-Type
    metric, err := relay.DecodeAs[Metric](resp)
    if err != nil {
        log.Fatal("decode error:", err)
    }
    fmt.Printf("metric: %s = %.2f tags=%v\n", metric.Name, metric.Value, metric.Tags)
}
```

---

## Type Constraints

All three decoder functions use the `any` constraint on the type parameter `T`. This means you can decode into any Go type. However, the actual value must be compatible with the chosen encoding:

```go
// Valid: struct with json tags
user, err := relay.DecodeJSON[User](resp)

// Valid: pointer to struct
userPtr, err := relay.DecodeJSON[*User](resp)

// Valid: slice of structs
users, err := relay.DecodeJSON[[]User](resp)

// Valid: map
data, err := relay.DecodeJSON[map[string]interface{}](resp)

// Valid: scalar types
count, err := relay.DecodeJSON[int](resp) // body: 42

// Valid: any interface
var raw interface{}
raw, err = relay.DecodeJSON[interface{}](resp)
```

---

## Zero Value Behavior on Error

When decoding fails, relay returns the zero value for `T` and a non-nil error. The zero value of any type in Go is:

| Type | Zero Value |
|------|-----------|
| struct | All fields zero-initialized |
| `*T` (pointer) | `nil` |
| `[]T` (slice) | `nil` |
| `map[K]V` | `nil` |
| `string` | `""` |
| `int`, `float64` | `0` |
| `bool` | `false` |

Always check the error before using the returned value:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

type Config struct {
    MaxWorkers int    `json:"max_workers"`
    Region     string `json:"region"`
    Debug      bool   `json:"debug"`
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    resp, err := client.Execute(context.Background(), client.Get("/config"))
    if err != nil {
        log.Fatal(err)
    }

    config, err := relay.DecodeJSON[Config](resp)
    if err != nil {
        // config is zero value here: Config{MaxWorkers: 0, Region: "", Debug: false}
        // Do NOT use config - it is invalid
        log.Fatal("failed to decode config:", err)
    }

    // Safe to use only after error check
    fmt.Printf("workers: %d, region: %s, debug: %v\n",
        config.MaxWorkers, config.Region, config.Debug)
}
```

---

## Error Handling for Decode Failures

Decode functions can fail for several reasons. Use `errors.As` to inspect the specific failure:

```go
package main

import (
    "context"
    "encoding/json"
    "encoding/xml"
    "errors"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

type Order struct {
    ID     string `json:"id"`
    Amount int    `json:"amount"`
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    resp, err := client.Execute(context.Background(), client.Get("/orders/latest"))
    if err != nil {
        log.Fatal(err)
    }

    order, decErr := relay.DecodeJSON[Order](resp)
    if decErr != nil {
        var syntaxErr *json.SyntaxError
        var typeErr *json.UnmarshalTypeError
        var xmlSyntaxErr *xml.SyntaxError

        switch {
        case errors.As(decErr, &syntaxErr):
            fmt.Printf("JSON syntax error at offset %d: %v\n", syntaxErr.Offset, syntaxErr)
        case errors.As(decErr, &typeErr):
            fmt.Printf("JSON type mismatch: field=%s expected=%s got=%s\n",
                typeErr.Field, typeErr.Type, typeErr.Value)
        case errors.As(decErr, &xmlSyntaxErr):
            fmt.Printf("XML syntax error at line %d: %v\n", xmlSyntaxErr.Line, xmlSyntaxErr)
        default:
            fmt.Printf("decode error: %v\n", decErr)
        }
        return
    }

    fmt.Printf("order: %s amount: %d\n", order.ID, order.Amount)
}
```

---

## Using Decoders with Pagination

Generic decoders integrate naturally with `relay.Paginate`:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

type Invoice struct {
    ID       string  `json:"id"`
    Amount   float64 `json:"amount"`
    Currency string  `json:"currency"`
    Status   string  `json:"status"`
}

func main() {
    client := relay.New(
        relay.WithBaseURL("https://billing.example.com"),
        relay.WithBearerToken("tok-123"),
    )

    // Paginate using DecodeJSON as the page decoder
    invoices, err := relay.Paginate[Invoice](
        context.Background(),
        client,
        client.Get("/invoices").WithQueryParam("per_page", "50"),
        func(resp *relay.Response) ([]Invoice, error) {
            return relay.DecodeJSON[[]Invoice](resp)
        },
    )
    if err != nil {
        log.Fatal("pagination error:", err)
    }

    var total float64
    for _, inv := range invoices {
        total += inv.Amount
    }
    fmt.Printf("total invoices: %d, total amount: %.2f\n", len(invoices), total)
}
```
