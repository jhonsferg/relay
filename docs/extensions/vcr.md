# VCR - HTTP Cassette Recording

`ext/vcr` provides **cassette recording and playback** for relay clients. During recording mode, real HTTP responses are saved to a YAML file (the "cassette"). During playback, the cassette is replayed without any network calls. This is ideal for integration tests, demos, and CI pipelines that must run without external dependencies.

## Installation

```bash
go get github.com/jhonsferg/relay/ext/vcr
```

## Basic Usage

### Recording a cassette

```go
import (
    "github.com/jhonsferg/relay"
    "github.com/jhonsferg/relay/ext/vcr"
)

client := relay.NewClient(
    vcr.WithVCR(vcr.Config{
        Cassette: "testdata/cassettes/users.yaml",
        Mode:     vcr.ModeRecord,
    }),
)

// This makes a real HTTP request and saves it to the cassette
resp, err := client.Execute(ctx, relay.NewRequest().GET("https://api.example.com/users"))
```

### Playing back a cassette

```go
client := relay.NewClient(
    vcr.WithVCR(vcr.Config{
        Cassette: "testdata/cassettes/users.yaml",
        Mode:     vcr.ModePlayback,
    }),
)

// Returns the saved response - no network call made
resp, err := client.Execute(ctx, relay.NewRequest().GET("https://api.example.com/users"))
```

### Auto mode (record once, replay forever)

```go
client := relay.NewClient(
    vcr.WithVCR(vcr.Config{
        Cassette: "testdata/cassettes/users.yaml",
        Mode:     vcr.ModeAuto, // record if cassette missing, replay if present
    }),
)
```

## Configuration

```go
type Config struct {
    // Path to the cassette YAML file
    Cassette string

    // Mode controls recording/playback behaviour
    Mode Mode

    // MatchOn controls which request attributes must match for playback
    // Default: [MatchMethod, MatchURL]
    MatchOn []Matcher

    // FilterHeaders removes sensitive headers before saving
    // e.g. []string{"Authorization", "X-API-Key"}
    FilterHeaders []string

    // FilterBody replaces sensitive patterns in request/response bodies
    FilterBody []BodyFilter
}
```

## Playback Modes

| Mode | Behaviour |
|------|-----------|
| `ModeRecord` | Always make real requests and record responses |
| `ModePlayback` | Only replay from cassette; fail if no matching interaction |
| `ModeAuto` | Record if cassette does not exist, replay if it does |
| `ModePassthrough` | Bypass VCR entirely (disables the extension) |

## Request Matching

By default, interactions are matched by HTTP method and full URL. You can customise this:

```go
vcr.Config{
    Cassette: "cassettes/search.yaml",
    Mode:     vcr.ModePlayback,
    MatchOn:  []vcr.Matcher{
        vcr.MatchMethod,
        vcr.MatchURL,
        vcr.MatchBody,   // also match request body
    },
}
```

Available matchers:

| Matcher | Description |
|---------|-------------|
| `MatchMethod` | HTTP method (GET, POST, etc.) |
| `MatchURL` | Full request URL including query string |
| `MatchURLPath` | URL path only (ignores query string) |
| `MatchBody` | Request body byte-for-byte |
| `MatchHeader(name)` | A specific request header value |

## Filtering Sensitive Data

Keep secrets out of cassette files:

```go
vcr.Config{
    Cassette: "cassettes/secure.yaml",
    Mode:     vcr.ModeRecord,
    FilterHeaders: []string{
        "Authorization",
        "X-API-Key",
        "Cookie",
    },
    FilterBody: []vcr.BodyFilter{
        {Pattern: `"password":"[^"]*"`, Replace: `"password":"[FILTERED]"`},
        {Pattern: `"token":"[^"]*"`,   Replace: `"token":"[FILTERED]"`},
    },
}
```

## Using in Tests

```go
func TestGetUser(t *testing.T) {
    client := relay.NewClient(
        vcr.WithVCR(vcr.Config{
            Cassette: "testdata/cassettes/get_user.yaml",
            Mode:     vcr.ModeAuto,
        }),
    )

    type User struct {
        ID   int    `json:"id"`
        Name string `json:"name"`
    }

    resp, err := client.Execute(t.Context(),
        relay.NewRequest().GET("https://api.example.com/users/1"),
    )
    if err != nil {
        t.Fatal(err)
    }

    var user User
    if err := resp.JSON(&user); err != nil {
        t.Fatal(err)
    }

    if user.Name != "Alice" {
        t.Errorf("expected Alice, got %s", user.Name)
    }
}
```

Run once with network access to create the cassette, then run offline forever.

## Cassette File Format

Cassettes are stored as human-readable YAML:

```yaml
interactions:
  - request:
      method: GET
      url: https://api.example.com/users/1
      headers:
        Accept: application/json
    response:
      status: 200
      headers:
        Content-Type: application/json
      body: '{"id":1,"name":"Alice"}'
      recorded_at: 2024-01-15T10:30:00Z
```

## CI/CD Pattern

Recommended workflow:

1. **Developers** run tests locally with `ModeAuto` - cassettes are created on first run.
2. **Cassettes are committed** to the repository under `testdata/cassettes/`.
3. **CI runs** with `ModePlayback` (or `ModeAuto`) - no external dependencies needed.
4. **Refresh cassettes** periodically by deleting them and re-running with network access.

```yaml
# .github/workflows/test.yml
- name: Run tests
  run: go test ./...
  # Cassettes in testdata/ are committed - no real network calls in CI
```

## Combining with Other Extensions

VCR works alongside other relay extensions:

```go
client := relay.NewClient(
    relay.WithRetry(relay.RetryConfig{MaxAttempts: 3}),
    vcr.WithVCR(vcr.Config{
        Cassette: "testdata/cassettes/flaky.yaml",
        Mode:     vcr.ModePlayback,
    }),
)
// VCR intercepts at transport layer - retries still work
```
