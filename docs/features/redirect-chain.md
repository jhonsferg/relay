# Redirect Chain Tracking

HTTP redirects are a common mechanism for URL normalization, authentication flows, and URL shortener resolution. By default, `relay` follows up to 10 redirects automatically, just like a browser. What sets `relay` apart is that it records every redirect hop along the way, giving you a complete audit trail of the path taken from the original URL to the final destination.

This is useful for debugging redirect loops, auditing URL shortener chains, monitoring CDN redirect patterns, and validating that your authentication flows are redirecting to the expected endpoints.

---

## RedirectChain on the Response

After a request completes (whether it was redirected or not), you can inspect the full redirect chain via the `RedirectChain()` method on the response:

```go
func (r *Response) RedirectChain() []RedirectInfo
```

If the request was not redirected, `RedirectChain()` returns an empty slice. If the request followed N redirects, it returns a slice of N `RedirectInfo` values, in order from the first redirect to the last, **not** including the final successful response.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://example.com"),
    )
    if err != nil {
        log.Fatal(err)
    }

    // This URL redirects: short.ly/abc -> marketing.example.com/promo -> example.com/landing
    resp, err := client.Get(context.Background(), "https://short.ly/abc", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    chain := resp.RedirectChain()
    fmt.Printf("followed %d redirect(s)\n", len(chain))
    for i, hop := range chain {
        fmt.Printf("  hop %d: %d %s\n", i+1, hop.StatusCode, hop.URL)
    }
    fmt.Printf("  final: %d %s\n", resp.StatusCode, resp.Request.URL)
}
```

---

## RedirectInfo Struct

Each hop in the redirect chain is represented by a `RedirectInfo`:

```go
type RedirectInfo struct {
    // URL is the full URL that was redirected to (the Location header value,
    // resolved relative to the request URL).
    URL string

    // StatusCode is the HTTP status code of the redirect response
    // (301, 302, 303, 307, or 308).
    StatusCode int

    // Header contains all response headers from the redirect response.
    // This includes Set-Cookie headers, which are important in auth flows.
    Header http.Header
}
```

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"

    "github.com/jhonsferg/relay"
)

func printRedirectInfo(hop relay.RedirectInfo) {
    fmt.Printf("  Status: %d (%s)\n", hop.StatusCode, http.StatusText(hop.StatusCode))
    fmt.Printf("  URL:    %s\n", hop.URL)
    if loc := hop.Header.Get("Location"); loc != "" {
        fmt.Printf("  Location: %s\n", loc)
    }
    if setCookie := hop.Header.Get("Set-Cookie"); setCookie != "" {
        fmt.Printf("  Set-Cookie: %s\n", setCookie)
    }
    fmt.Println()
}

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://auth.example.com"),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/login?return_to=/dashboard", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    chain := resp.RedirectChain()
    fmt.Printf("Redirect chain (%d hops):\n\n", len(chain))
    for _, hop := range chain {
        printRedirectInfo(hop)
    }
    fmt.Printf("Final response: %d %s\n", resp.StatusCode, resp.Request.URL)
}
```

---

## Default Redirect Follow Behavior

By default, `relay` follows up to 10 redirects automatically. This matches the behavior of `curl` and most web browsers. The 10-hop limit prevents infinite redirect loops from consuming resources indefinitely.

If the limit is exceeded, `relay` returns an error describing the loop:

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://example.com"),
        // Default: follows up to 10 redirects
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/might-loop", nil)
    if err != nil {
        var redirectErr *relay.TooManyRedirectsError
        if errors.As(err, &redirectErr) {
            fmt.Printf("redirect loop detected after %d hops\n", redirectErr.Hops)
            fmt.Println("chain:")
            for i, hop := range redirectErr.Chain {
                fmt.Printf("  %d: %d %s\n", i+1, hop.StatusCode, hop.URL)
            }
            return
        }
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("final status:", resp.StatusCode)
}
```

---

## WithMaxRedirects

```go
func WithMaxRedirects(n int) Option
```

Override the default maximum redirect limit. Set a lower value to be more conservative, or a higher value if you know a particular flow involves many legitimate redirect hops.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    // Some legacy systems have auth flows with many redirect hops.
    // Allow up to 20 redirects for this specific client.
    deepRedirectClient, err := relay.New(
        relay.WithBaseURL("https://legacy-sso.example.com"),
        relay.WithMaxRedirects(20),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := deepRedirectClient.Get(context.Background(), "/sso/initiate", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    chain := resp.RedirectChain()
    fmt.Printf("SSO flow completed after %d redirect(s)\n", len(chain))
    fmt.Println("final URL:", resp.Request.URL)

    // Shallow redirect client: any more than 2 hops is suspicious
    strictClient, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithMaxRedirects(2),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp2, err := strictClient.Get(context.Background(), "/resource", nil)
    if err != nil {
        log.Fatal("unexpected deep redirect:", err)
    }
    defer resp2.Body.Close()
    fmt.Println("api response:", resp2.StatusCode)
}
```

> **tip**
> For REST API clients, consider setting `WithMaxRedirects(1)` or even `WithMaxRedirects(0)` (no redirects). Well-designed APIs should not redirect. If they do, something unexpected may be happening (e.g., load balancer misconfiguration, wrong base URL).

---

## WithDisableRedirect

```go
func WithDisableRedirect() Option
```

Disables automatic redirect following entirely. When the server responds with a 3xx status code, `relay` returns that response directly to the caller without following the `Location` header.

This is useful when you need to inspect the raw redirect response, handle redirects in your own application logic, or when working with APIs that use 302/303 to signal something other than a true redirect.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"

    "github.com/jhonsferg/relay"
)

func main() {
    // Do not follow redirects - we want to inspect them manually.
    client, err := relay.New(
        relay.WithBaseURL("https://files.example.com"),
        relay.WithDisableRedirect(),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/download/report-2024.pdf", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
        // Extract the pre-signed S3 URL from the Location header
        presignedURL := resp.Header.Get("Location")
        fmt.Println("pre-signed download URL:", presignedURL)

        // Now decide whether to follow: validate the domain, log it, etc.
        // Then issue the actual download request with a standard http.Client
        // to avoid following more redirects unintentionally.
        return
    }

    fmt.Println("direct download, status:", resp.StatusCode)
}
```

---

## Inspecting the Chain After a Request

Here is a complete example that inspects the redirect chain and logs a structured summary:

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "os"
    "time"

    "github.com/jhonsferg/relay"
)

type RedirectAudit struct {
    OriginalURL  string            `json:"original_url"`
    FinalURL     string            `json:"final_url"`
    FinalStatus  int               `json:"final_status"`
    HopCount     int               `json:"hop_count"`
    TotalLatency string            `json:"total_latency"`
    Hops         []RedirectHopLog  `json:"hops"`
}

type RedirectHopLog struct {
    Position   int    `json:"position"`
    StatusCode int    `json:"status_code"`
    URL        string `json:"url"`
}

func auditRedirects(ctx context.Context, client *relay.Client, originalURL string) (*RedirectAudit, error) {
    start := time.Now()
    resp, err := client.Get(ctx, originalURL, nil)
    if err != nil {
        return nil, fmt.Errorf("request failed: %w", err)
    }
    defer resp.Body.Close()
    elapsed := time.Since(start)

    chain := resp.RedirectChain()
    audit := &RedirectAudit{
        OriginalURL:  originalURL,
        FinalURL:     resp.Request.URL.String(),
        FinalStatus:  resp.StatusCode,
        HopCount:     len(chain),
        TotalLatency: elapsed.String(),
        Hops:         make([]RedirectHopLog, len(chain)),
    }
    for i, hop := range chain {
        audit.Hops[i] = RedirectHopLog{
            Position:   i + 1,
            StatusCode: hop.StatusCode,
            URL:        hop.URL,
        }
    }
    return audit, nil
}

func main() {
    client, err := relay.New(
        relay.WithMaxRedirects(15),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Audit a shortened URL
    audit, err := auditRedirects(context.Background(), client, "https://bit.ly/3example")
    if err != nil {
        log.Fatal(err)
    }

    enc := json.NewEncoder(os.Stdout)
    enc.SetIndent("", "  ")
    if err := enc.Encode(audit); err != nil {
        log.Fatal(err)
    }
}
```

---

## Use Case: Audit Trail for URL Shorteners

URL shorteners (bit.ly, t.co, ow.ly) can chain multiple redirects through analytics trackers, affiliate redirect services, and CDN edges before reaching the final page. Building a link scanner or redirect resolver is straightforward with `relay`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net/url"
    "strings"
    "time"

    "github.com/jhonsferg/relay"
)

type ResolvedLink struct {
    Input       string
    FinalURL    string
    Hops        int
    IsSuspicious bool
    Reasons     []string
}

var suspiciousDomains = []string{
    "malware-site.example",
    "phishing.example",
}

func isSuspicious(u string) (bool, string) {
    parsed, err := url.Parse(u)
    if err != nil {
        return false, ""
    }
    host := strings.ToLower(parsed.Hostname())
    for _, bad := range suspiciousDomains {
        if strings.HasSuffix(host, bad) {
            return true, "known malicious domain: " + bad
        }
    }
    return false, ""
}

func resolveLink(ctx context.Context, client *relay.Client, shortURL string) (*ResolvedLink, error) {
    resp, err := client.Get(ctx, shortURL, nil)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    chain := resp.RedirectChain()
    result := &ResolvedLink{
        Input:    shortURL,
        FinalURL: resp.Request.URL.String(),
        Hops:     len(chain),
    }

    // Check every hop for suspicious domains
    for _, hop := range chain {
        if sus, reason := isSuspicious(hop.URL); sus {
            result.IsSuspicious = true
            result.Reasons = append(result.Reasons, reason)
        }
    }
    if sus, reason := isSuspicious(result.FinalURL); sus {
        result.IsSuspicious = true
        result.Reasons = append(result.Reasons, reason)
    }

    return result, nil
}

func main() {
    // Use a client that follows many redirects but has a short timeout
    client, err := relay.New(
        relay.WithMaxRedirects(20),
        relay.WithTimeout(10*time.Second),
    )
    if err != nil {
        log.Fatal(err)
    }

    urls := []string{
        "https://bit.ly/example1",
        "https://tinyurl.com/example2",
        "https://ow.ly/example3",
    }

    for _, u := range urls {
        result, err := resolveLink(context.Background(), client, u)
        if err != nil {
            fmt.Printf("ERROR %s: %v\n", u, err)
            continue
        }
        status := "CLEAN"
        if result.IsSuspicious {
            status = "SUSPICIOUS: " + strings.Join(result.Reasons, ", ")
        }
        fmt.Printf("[%s] %s -> %s (%d hops)\n", status, result.Input, result.FinalURL, result.Hops)
    }
}
```

> **warning**
> When resolving untrusted URLs (e.g., user-submitted links), set a strict `WithTimeout` and a conservative `WithMaxRedirects` to prevent your service from hanging on redirect loops or slow redirect chains. Always validate the final destination URL before presenting it to users.

---

## Summary

| Feature | API |
|---|---|
| Read redirect chain | `resp.RedirectChain()` returns `[]RedirectInfo` |
| Hop details | `RedirectInfo{URL, StatusCode, Header}` |
| Default limit | 10 redirects |
| Custom limit | `WithMaxRedirects(n)` |
| Disable redirect following | `WithDisableRedirect()` |
| Detect redirect loops | Check for `*relay.TooManyRedirectsError` |
| URL shortener auditing | Iterate `resp.RedirectChain()` after GET |
