# GraphQL Extension

The GraphQL extension provides a type-safe, generic API for executing GraphQL queries and mutations through a relay `Client`. It handles request encoding, response decoding, and GraphQL-specific error extraction, including partial data responses where the `data` field is non-null alongside an `errors` array.

**Import path:** `github.com/jhonsferg/relay/ext/graphql`

---

## Overview

GraphQL APIs accept POST requests with a JSON body containing `query`, `variables`, and optionally `operationName`. Responses always return HTTP 200 with a JSON body that may include both `data` and `errors`. The extension wraps these mechanics so you can work with strongly-typed Go structs and receive meaningful error values.

Key capabilities:
- Generic `Query[T]` and `Mutate[T]` functions that decode directly into your result type
- Partial data handling - access successfully resolved fields even when some resolvers fail
- Named fragment support via inline fragment definitions in query strings
- All relay middleware applies transparently (retry, auth, caching, observability)
- No code generation required

---

## Installation

```bash
go get github.com/jhonsferg/relay/ext/graphql@latest
```

---

## API Reference

### `relaygraphql.Query`

```go
func Query[T any](
    ctx       context.Context,
    client    *relay.Client,
    query     string,
    variables map[string]any,
    result    *T,
) error
```

Executes a GraphQL query operation. The generic type parameter `T` must match the shape of the `data` field in the response. The function returns `nil` only when the response contains a non-null `data` field and no `errors`. If there are errors but `data` is also present, it returns a `*PartialError` that lets you access both.

### `relaygraphql.Mutate`

```go
func Mutate[T any](
    ctx       context.Context,
    client    *relay.Client,
    mutation  string,
    variables map[string]any,
    result    *T,
) error
```

Identical to `Query` but signals to the extension that this is a mutation operation. This distinction matters when combined with middleware that treats mutations differently (for example, cache invalidation middleware that purges cached responses after a successful mutation).

### `relaygraphql.Subscribe`

```go
func Subscribe[T any](
    ctx      context.Context,
    client   *relay.Client,
    sub      string,
    variables map[string]any,
    handler  func(event T) error,
) error
```

Initiates a GraphQL subscription over Server-Sent Events (SSE). Each server-sent event is decoded into `T` and passed to `handler`. The subscription runs until the context is cancelled or `handler` returns a non-nil error. See [Subscription Support](#subscription-support) for details.

---

## Error Types

### `GraphQLError`

Represents a single entry in the `errors` array of a GraphQL response:

```go
type GraphQLError struct {
    Message    string                 `json:"message"`
    Locations  []ErrorLocation        `json:"locations,omitempty"`
    Path       []any                  `json:"path,omitempty"`
    Extensions map[string]any         `json:"extensions,omitempty"`
}

type ErrorLocation struct {
    Line   int `json:"line"`
    Column int `json:"column"`
}
```

### `PartialError`

Returned when the response contains both `data` and `errors`:

```go
type PartialError[T any] struct {
    Data   T              // partially resolved data
    Errors []GraphQLError // list of resolver errors
}

func (e *PartialError[T]) Error() string
```

Use `errors.As` to unwrap a `PartialError` and access the partial data:

```go
var result MyData
err := relaygraphql.Query(ctx, client, query, vars, &result)
if err != nil {
    var partial *relaygraphql.PartialError[MyData]
    if errors.As(err, &partial) {
        // partial.Data contains what resolved successfully
        // partial.Errors contains what failed
        for _, e := range partial.Errors {
            log.Printf("resolver error at %v: %s", e.Path, e.Message)
        }
        // optionally continue using partial.Data
    } else {
        // transport error or complete failure
        return err
    }
}
```

---

## Complete Example: GitHub GraphQL API v4

This example queries the GitHub GraphQL API for information about a repository, including its description, star count, recent issues, and primary language.

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "log"
    "os"
    "time"

    relay "github.com/jhonsferg/relay"
    relaygraphql "github.com/jhonsferg/relay/ext/graphql"
)

// Define Go types matching the GraphQL response shape.

type Language struct {
    Name  string `json:"name"`
    Color string `json:"color"`
}

type Issue struct {
    Number int    `json:"number"`
    Title  string `json:"title"`
    URL    string `json:"url"`
    State  string `json:"state"`
}

type IssueEdge struct {
    Node Issue `json:"node"`
}

type IssueConnection struct {
    TotalCount int         `json:"totalCount"`
    Edges      []IssueEdge `json:"edges"`
}

type Repository struct {
    Name            string          `json:"name"`
    Description     string          `json:"description"`
    StargazerCount  int             `json:"stargazerCount"`
    ForkCount       int             `json:"forkCount"`
    IsPrivate       bool            `json:"isPrivate"`
    URL             string          `json:"url"`
    PrimaryLanguage Language        `json:"primaryLanguage"`
    Issues          IssueConnection `json:"issues"`
}

type RepoQueryResult struct {
    Repository Repository `json:"repository"`
}

const repoQuery = `
query GetRepository($owner: String!, $name: String!, $issueCount: Int!) {
  repository(owner: $owner, name: $name) {
    name
    description
    stargazerCount
    forkCount
    isPrivate
    url
    primaryLanguage {
      name
      color
    }
    issues(first: $issueCount, states: [OPEN], orderBy: {field: CREATED_AT, direction: DESC}) {
      totalCount
      edges {
        node {
          number
          title
          url
          state
        }
      }
    }
  }
}
`

func main() {
    token := os.Getenv("GITHUB_TOKEN")
    if token == "" {
        log.Fatal("GITHUB_TOKEN environment variable not set")
    }

    client, err := relay.NewClient(
        relay.WithBaseURL("https://api.github.com"),
        relay.WithHeader("Authorization", "Bearer "+token),
        relay.WithHeader("Accept", "application/vnd.github+json"),
        relay.WithHeader("X-GitHub-Api-Version", "2022-11-28"),
        relay.WithTimeout(15*time.Second),
    )
    if err != nil {
        log.Fatalf("create client: %v", err)
    }

    vars := map[string]any{
        "owner":      "golang",
        "name":       "go",
        "issueCount": 5,
    }

    var result RepoQueryResult
    err = relaygraphql.Query(context.Background(), client, repoQuery, vars, &result)
    if err != nil {
        var partial *relaygraphql.PartialError[RepoQueryResult]
        if errors.As(err, &partial) {
            fmt.Println("Partial response received:")
            for _, e := range partial.Errors {
                fmt.Printf("  - error at path %v: %s\n", e.Path, e.Message)
            }
            // Still print what we got.
            printRepo(partial.Data.Repository)
        } else {
            log.Fatalf("query: %v", err)
        }
        return
    }

    printRepo(result.Repository)
}

func printRepo(r Repository) {
    fmt.Printf("Repository: %s\n", r.Name)
    fmt.Printf("Description: %s\n", r.Description)
    fmt.Printf("Stars: %d  Forks: %d\n", r.StargazerCount, r.ForkCount)
    fmt.Printf("Language: %s (%s)\n", r.PrimaryLanguage.Name, r.PrimaryLanguage.Color)
    fmt.Printf("Open issues: %d (showing %d)\n", r.Issues.TotalCount, len(r.Issues.Edges))
    for _, edge := range r.Issues.Edges {
        fmt.Printf("  #%d [%s] %s\n", edge.Node.Number, edge.Node.State, edge.Node.Title)
    }
}
```

---

## Mutation Example: Adding a Star

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    relay "github.com/jhonsferg/relay"
    relaygraphql "github.com/jhonsferg/relay/ext/graphql"
)

type StarResult struct {
    AddStar struct {
        Starrable struct {
            StargazerCount int `json:"stargazerCount"`
        } `json:"starrable"`
    } `json:"addStar"`
}

const addStarMutation = `
mutation AddStar($starrableId: ID!) {
  addStar(input: {starrableId: $starrableId}) {
    starrable {
      stargazerCount
    }
  }
}
`

func main() {
    client, _ := relay.NewClient(
        relay.WithBaseURL("https://api.github.com"),
        relay.WithHeader("Authorization", "Bearer "+os.Getenv("GITHUB_TOKEN")),
        relay.WithTimeout(10*time.Second),
    )

    var result StarResult
    err := relaygraphql.Mutate(
        context.Background(),
        client,
        addStarMutation,
        map[string]any{"starrableId": "MDEwOlJlcG9zaXRvcnkyMzA5Njk1OQ=="},
        &result,
    )
    if err != nil {
        log.Fatalf("mutate: %v", err)
    }

    fmt.Printf("Stargazers after starring: %d\n",
        result.AddStar.Starrable.StargazerCount)
}
```

---

## Fragment Support

GraphQL fragments are defined inline within your query string. The extension passes the full query string verbatim to the server, so any fragments you define are included automatically:

```go
const queryWithFragments = `
fragment RepoFields on Repository {
  name
  description
  stargazerCount
  url
}

fragment LanguageFields on Language {
  name
  color
}

query GetMultipleRepos($owner: String!) {
  goRepo: repository(owner: $owner, name: "go") {
    ...RepoFields
    primaryLanguage {
      ...LanguageFields
    }
  }
  pkgSiteRepo: repository(owner: $owner, name: "pkgsite") {
    ...RepoFields
    primaryLanguage {
      ...LanguageFields
    }
  }
}
`

type MultiRepoResult struct {
    GoRepo      Repository `json:"goRepo"`
    PkgSiteRepo Repository `json:"pkgSiteRepo"`
}

var result MultiRepoResult
err := relaygraphql.Query(ctx, client, queryWithFragments,
    map[string]any{"owner": "golang"}, &result)
```

> **Tip:** Keep fragment definitions in Go constants and compose queries at package initialization time. This keeps your query strings readable and allows static analysis tools to validate them.

---

## Named Operations

When your query string contains multiple operations, you must specify which one to execute using `relaygraphql.WithOperationName`:

```go
err := relaygraphql.Query(ctx, client, multiOpQuery, vars, &result,
    relaygraphql.WithOperationName("GetRepository"),
)
```

---

## Custom Endpoint

By default the extension posts to `/graphql`. Override this with `relaygraphql.WithEndpoint`:

```go
err := relaygraphql.Query(ctx, client, query, vars, &result,
    relaygraphql.WithEndpoint("/api/v2/graphql"),
)
```

---

## Persisted Queries (Automatic Persisted Queries)

Enable APQ to reduce request sizes for frequently used queries:

```go
err := relaygraphql.Query(ctx, client, query, vars, &result,
    relaygraphql.WithPersistedQuery(queryHash),
)
```

On the first request relay sends only the hash. If the server returns a `PersistedQueryNotFound` error, relay automatically retries with the full query text. Subsequent requests use the hash only.

---

## Subscription Support

> **Note:** GraphQL subscriptions require a server that supports SSE or WebSocket transport. The relay extension uses SSE (Server-Sent Events) by default, which works over standard HTTP connections. WebSocket support is available as a separate configuration option.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    relay "github.com/jhonsferg/relay"
    relaygraphql "github.com/jhonsferg/relay/ext/graphql"
)

type ReviewEvent struct {
    PullRequestReview struct {
        ID     string `json:"id"`
        State  string `json:"state"`
        Author struct {
            Login string `json:"login"`
        } `json:"author"`
    } `json:"pullRequestReview"`
}

const reviewSub = `
subscription OnPullRequestReview($prId: ID!) {
  pullRequestReview(pullRequestId: $prId) {
    id
    state
    author {
      login
    }
  }
}
`

func main() {
    client, _ := relay.NewClient(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithHeader("Authorization", "Bearer token"),
    )

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()

    err := relaygraphql.Subscribe(ctx, client, reviewSub,
        map[string]any{"prId": "PR_123"},
        func(event ReviewEvent) error {
            fmt.Printf("Review by %s: %s\n",
                event.PullRequestReview.Author.Login,
                event.PullRequestReview.State)
            return nil
        },
    )
    if err != nil {
        log.Fatalf("subscribe: %v", err)
    }
}
```

> **Warning:** Long-lived subscriptions bypass relay's standard request timeout. Set an explicit context deadline as shown above to prevent goroutine leaks.

---

## Testing GraphQL Clients

Use the mock transport to test GraphQL logic without a real server:

```go
package mypackage_test

import (
    "context"
    "encoding/json"
    "io"
    "net/http"
    "strings"
    "testing"

    relay "github.com/jhonsferg/relay"
    relaygraphql "github.com/jhonsferg/relay/ext/graphql"
    relaymock "github.com/jhonsferg/relay/ext/mock"
)

func TestRepoQuery(t *testing.T) {
    transport := relaymock.NewTransport()

    responseBody := map[string]any{
        "data": map[string]any{
            "repository": map[string]any{
                "name":           "go",
                "description":    "The Go programming language",
                "stargazerCount": 120000,
                "forkCount":      17000,
                "isPrivate":      false,
                "url":            "https://github.com/golang/go",
                "primaryLanguage": map[string]any{
                    "name":  "Go",
                    "color": "#00ADD8",
                },
                "issues": map[string]any{
                    "totalCount": 8000,
                    "edges":      []any{},
                },
            },
        },
    }

    rawBody, _ := json.Marshal(responseBody)
    transport.EnqueueFunc(func(req *http.Request) (*http.Response, error) {
        // Verify the request contains the expected variables.
        var body map[string]any
        _ = json.NewDecoder(req.Body).Decode(&body)
        vars, _ := body["variables"].(map[string]any)
        if vars["owner"] != "golang" {
            t.Errorf("expected owner=golang, got %v", vars["owner"])
        }
        return &http.Response{
            StatusCode: http.StatusOK,
            Header:     http.Header{"Content-Type": []string{"application/json"}},
            Body:       io.NopCloser(strings.NewReader(string(rawBody))),
        }, nil
    })

    client, _ := relay.NewClient(
        relay.WithBaseURL("https://api.github.com"),
        relay.WithTransport(transport),
    )

    var result RepoQueryResult
    err := relaygraphql.Query(context.Background(), client, repoQuery,
        map[string]any{"owner": "golang", "name": "go", "issueCount": 0},
        &result)
    if err != nil {
        t.Fatalf("query: %v", err)
    }
    if result.Repository.Name != "go" {
        t.Errorf("expected name=go, got %q", result.Repository.Name)
    }
    if result.Repository.StargazerCount != 120000 {
        t.Errorf("unexpected star count: %d", result.Repository.StargazerCount)
    }
}
```

---

## See Also

- [Mock Transport Extension](mock.md) - unit testing without real servers
- [OAuth2 Extension](oauth.md) - authenticating GraphQL requests
- [Redis Cache Extension](cache.md) - caching GraphQL query responses
