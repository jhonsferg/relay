# Pagination

relay has first-class support for paginating HTTP APIs. The two entry points
are:

| Method | Use when |
|--------|----------|
| `client.Paginate` | The server uses RFC 5988 `Link` headers (`rel="next"`) |
| `client.PaginateWith` | You need a custom strategy to locate the next page |

Both methods collect results through a caller-supplied `PageFunc` and stop when
the `PageFunc` returns `(false, nil)` or the next-page URL is empty.

---

## Core types

```go
// PageFunc is called for each page.
// Return (true, nil) to continue, (false, nil) to stop, (false, err) to abort.
type PageFunc func(resp *relay.Response) (more bool, err error)

// NextPageFunc extracts the URL for the next page from a response.
// Return an empty string to signal the last page.
type NextPageFunc func(resp *relay.Response) string
```

---

## Paginate - RFC 5988 Link header (built-in)

`client.Paginate` parses the `Link` response header and follows the URL marked
`rel="next"` automatically. This matches the pagination style used by GitHub,
GitLab, Bitbucket, and many other REST APIs.

```
Link: <https://api.example.com/users?page=2>; rel="next",
      <https://api.example.com/users?page=9>; rel="last"
```

### Basic usage

```go
package main

import (
    "context"
    "fmt"

    relay "github.com/jhonsferg/relay"
)

type User struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))
    defer client.Shutdown(context.Background()) //nolint:errcheck

    var allUsers []User

    err := client.Paginate(
        context.Background(),
        client.Get("/users").QueryParam("per_page", "100"),
        func(resp *relay.Response) (bool, error) {
            var page []User
            if err := resp.JSON(&page); err != nil {
                return false, err
            }
            allUsers = append(allUsers, page...)
            return true, nil // continue to next page
        },
    )
    if err != nil {
        fmt.Println("pagination error:", err)
        return
    }

    fmt.Printf("fetched %d users across all pages\n", len(allUsers))
}
```

---

## Stopping pagination early

Return `(false, nil)` from the `PageFunc` to stop before the last page. This
is useful when you only need the first N results or when a search condition is
satisfied:

```go
package main

import (
    "context"
    "fmt"

    relay "github.com/jhonsferg/relay"
)

type Issue struct {
    ID    int    `json:"id"`
    Title string `json:"title"`
    State string `json:"state"`
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.github.com"))
    defer client.Shutdown(context.Background()) //nolint:errcheck

    var openIssues []Issue
    const limit = 50

    err := client.Paginate(
        context.Background(),
        client.Get("/repos/octocat/hello-world/issues").
            QueryParam("state", "open").
            QueryParam("per_page", "30"),
        func(resp *relay.Response) (bool, error) {
            var page []Issue
            if err := resp.JSON(&page); err != nil {
                return false, err
            }
            openIssues = append(openIssues, page...)

            // Stop once we have enough results.
            if len(openIssues) >= limit {
                return false, nil
            }
            return true, nil
        },
    )
    if err != nil {
        fmt.Println("error:", err)
        return
    }

    if len(openIssues) > limit {
        openIssues = openIssues[:limit]
    }
    fmt.Printf("collected %d open issues\n", len(openIssues))
}
```

---

## PaginateWith - custom next-page logic

Use `client.PaginateWith` when the API uses a mechanism other than `Link`
headers - for example a `next_cursor` JSON field or a page-number query
parameter:

### Cursor-based pagination (JSON body)

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"

    relay "github.com/jhonsferg/relay"
)

type Event struct {
    ID   string `json:"id"`
    Type string `json:"type"`
}

type EventsPage struct {
    Events     []Event `json:"events"`
    NextCursor string  `json:"next_cursor"`
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))
    defer client.Shutdown(context.Background()) //nolint:errcheck

    var allEvents []Event

    // nextFn reads the cursor from the JSON body and builds the next URL.
    nextFn := func(resp *relay.Response) string {
        var page EventsPage
        if err := json.Unmarshal(resp.Body(), &page); err != nil {
            return ""
        }
        if page.NextCursor == "" {
            return ""
        }
        return "/events?cursor=" + page.NextCursor
    }

    err := client.PaginateWith(
        context.Background(),
        client.Get("/events"),
        nextFn,
        func(resp *relay.Response) (bool, error) {
            var page EventsPage
            if err := resp.JSON(&page); err != nil {
                return false, err
            }
            allEvents = append(allEvents, page.Events...)
            return true, nil
        },
    )
    if err != nil {
        fmt.Println("error:", err)
        return
    }

    fmt.Printf("total events: %d\n", len(allEvents))
}
```

### Page-number query parameter

```go
package main

import (
    "context"
    "fmt"
    "strconv"

    relay "github.com/jhonsferg/relay"
)

type Product struct {
    ID    int    `json:"id"`
    Name  string `json:"name"`
    Price int    `json:"price"`
}

type ProductsResponse struct {
    Products   []Product `json:"products"`
    TotalPages int       `json:"total_pages"`
    Page       int       `json:"page"`
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))
    defer client.Shutdown(context.Background()) //nolint:errcheck

    var allProducts []Product

    nextFn := func(resp *relay.Response) string {
        var pr ProductsResponse
        if err := resp.JSON(&pr); err != nil {
            return ""
        }
        if pr.Page >= pr.TotalPages {
            return "" // last page
        }
        return "/products?page=" + strconv.Itoa(pr.Page+1) + "&per_page=50"
    }

    err := client.PaginateWith(
        context.Background(),
        client.Get("/products").QueryParam("page", "1").QueryParam("per_page", "50"),
        nextFn,
        func(resp *relay.Response) (bool, error) {
            var pr ProductsResponse
            if err := resp.JSON(&pr); err != nil {
                return false, err
            }
            allProducts = append(allProducts, pr.Products...)
            return true, nil
        },
    )
    if err != nil {
        fmt.Println("error:", err)
        return
    }

    fmt.Printf("loaded %d products\n", len(allProducts))
}
```

---

## Complete example: paginating a GitHub-style API

The following example retrieves all open pull requests from a GitHub repository,
collecting them page by page and respecting a context deadline:

```go
package main

import (
    "context"
    "fmt"
    "os"
    "time"

    relay "github.com/jhonsferg/relay"
)

type PullRequest struct {
    Number int    `json:"number"`
    Title  string `json:"title"`
    State  string `json:"state"`
    User   struct {
        Login string `json:"login"`
    } `json:"user"`
    CreatedAt string `json:"created_at"`
}

func fetchAllPRs(token, owner, repo string) ([]PullRequest, error) {
    client := relay.New(
        relay.WithBaseURL("https://api.github.com"),
        relay.WithDefaultHeader("Authorization", "Bearer "+token),
        relay.WithDefaultHeader("Accept", "application/vnd.github+json"),
        relay.WithDefaultHeader("X-GitHub-Api-Version", "2022-11-28"),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
    defer cancel()

    var all []PullRequest

    err := client.Paginate(
        ctx,
        client.Get(fmt.Sprintf("/repos/%s/%s/pulls", owner, repo)).
            QueryParam("state", "open").
            QueryParam("per_page", "100").
            WithContext(ctx),
        func(resp *relay.Response) (bool, error) {
            if !resp.IsSuccess() {
                return false, fmt.Errorf("GitHub API error: %s", resp.Status())
            }

            var page []PullRequest
            if err := resp.JSON(&page); err != nil {
                return false, fmt.Errorf("decode page: %w", err)
            }

            all = append(all, page...)
            fmt.Printf("  fetched page, total so far: %d\n", len(all))

            // Return false when this page is empty (safety guard).
            return len(page) > 0, nil
        },
    )
    return all, err
}

func main() {
    token := os.Getenv("GITHUB_TOKEN")
    if token == "" {
        fmt.Fprintln(os.Stderr, "GITHUB_TOKEN is not set")
        os.Exit(1)
    }

    prs, err := fetchAllPRs(token, "golang", "go")
    if err != nil {
        fmt.Fprintln(os.Stderr, "error:", err)
        os.Exit(1)
    }

    fmt.Printf("total open pull requests: %d\n", len(prs))
    for i, pr := range prs {
        if i >= 5 {
            break
        }
        fmt.Printf("  #%d  %-60s  by %s\n", pr.Number, pr.Title, pr.User.Login)
    }
}
```

---

## Collecting all results into a slice

A common pattern is to start with a `nil` slice and append in every `PageFunc`
call. Because relay calls `PageFunc` once per page and waits for each HTTP
response before calling the next, you do not need any synchronisation:

```go
package main

import (
    "context"
    "fmt"

    relay "github.com/jhonsferg/relay"
)

type Tag struct {
    Name string `json:"name"`
}

func fetchAllTags(client *relay.Client, owner, repo string) ([]Tag, error) {
    var tags []Tag

    err := client.Paginate(
        context.Background(),
        client.Get(fmt.Sprintf("/repos/%s/%s/tags", owner, repo)).
            QueryParam("per_page", "100"),
        func(resp *relay.Response) (bool, error) {
            var page []Tag
            if err := resp.JSON(&page); err != nil {
                return false, err
            }
            tags = append(tags, page...)
            return len(page) == 100, nil // stop when page is not full
        },
    )
    return tags, err
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.github.com"))
    defer client.Shutdown(context.Background()) //nolint:errcheck

    tags, err := fetchAllTags(client, "jhonsferg", "relay")
    if err != nil {
        fmt.Println("error:", err)
        return
    }
    fmt.Printf("found %d tags\n", len(tags))
}
```

> **Tip**
> Return `(len(page) == pageSize, nil)` from your `PageFunc` as a cheap
> heuristic to stop early: if the last page is smaller than the requested page
> size, there are no more pages, so no extra round trip is needed.

---

## Error handling during pagination

When the `PageFunc` returns a non-nil error, `Paginate` / `PaginateWith`
stops immediately and returns that error to the caller. HTTP transport errors
(timeouts, connection failures, circuit opens) also stop pagination and
propagate as the return value:

```go
package main

import (
    "context"
    "errors"
    "fmt"

    relay "github.com/jhonsferg/relay"
)

type Item struct {
    ID int `json:"id"`
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))
    defer client.Shutdown(context.Background()) //nolint:errcheck

    var items []Item

    err := client.Paginate(
        context.Background(),
        client.Get("/items").QueryParam("per_page", "50"),
        func(resp *relay.Response) (bool, error) {
            if resp.StatusCode == 401 {
                return false, errors.New("not authorised - check API key")
            }
            if resp.StatusCode == 429 {
                return false, errors.New("rate limited - back off and retry later")
            }

            var page []Item
            if err := resp.JSON(&page); err != nil {
                return false, fmt.Errorf("page decode: %w", err)
            }
            items = append(items, page...)
            return true, nil
        },
    )

    if err != nil {
        fmt.Println("pagination stopped:", err)
    } else {
        fmt.Printf("collected %d items\n", len(items))
    }
}
```

> **Note**
> `Paginate` and `PaginateWith` do not perform any retries themselves - retry
> logic lives at the `Execute` level. Configure `WithRetry` on the client to
> automatically retry transient failures on individual page fetches.
