# Session & Cookie Management

relay v0.4.0 introduces automatic session management with transparent cookie jar support, enabling stateful protocols like CSRF token flows with SAP and other enterprise services.

## Automatic Cookie Jar (v0.4.0+)

All relay clients now initialize with a default `http.CookieJar` automatically. This means:

- ✅ Cookies from `Set-Cookie` headers are captured automatically
- ✅ Captured cookies are included in subsequent requests
- ✅ Cookie storage and expiration is managed transparently
- ✅ No manual cookie management needed in application code
- ✅ 100% backward compatible

### How It Works

```go
import (
    "context"
    "github.com/jhonsferg/relay"
)

// Client is initialized with http.CookieJar by default
client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
)

// Request 1: Server sends Set-Cookie in response
// Cookies are automatically captured and stored
_, _ = relay.Get[Response](ctx, client, "/login", &resp)

// Request 2: Stored cookies are automatically included
// No manual cookie management needed
_, _ = relay.Post[Request, Response](ctx, client, "/protected", req, &resp)
```

### Custom Cookie Jar

If you need custom cookie jar behavior, provide your own:

```go
import (
    "net/http/cookiejar"
    "github.com/jhonsferg/relay"
)

// Custom cookie jar with specific options
jar, _ := cookiejar.New(&cookiejar.Options{
    PublicSuffixList: nil, // or your custom list
})

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relay.WithCookieJar(jar),
)
```

## CSRF Token + Cookie Workflows

The automatic cookie jar enables atomic CSRF token + cookie handling, essential for SAP and other enterprise APIs:

### SAP Gateway CSRF Flow

```go
import (
    "context"
    "github.com/jhonsferg/traverse"
    "github.com/jhonsferg/traverse/ext/sap"
)

// Client with automatic cookie management
client, _ := traverse.New(
    traverse.WithBaseURL("https://sap.example.com/sap/opu/odata/sap/API_SALES_ORDER_SRV/"),
    sap.WithCSRFMiddleware(),
)

// Phase 1: Token fetch (cookies are captured automatically)
// GET $metadata with X-CSRF-Token: Fetch
// Server responds with:
//   - X-CSRF-Token header
//   - Set-Cookie header(s)
// Both are captured automatically

// Phase 2: Mutating request (cookies are included automatically)
// POST with:
//   - X-CSRF-Token header (from phase 1)
//   - Cookie header (captured in phase 1, included automatically)
order := Order{SalesOrderType: "TA"}
created, err := sap.CreateJsonAs[Order](
    client.From("A_SalesOrder"),
    context.Background(),
    order,
)

// Phase 3: Token expiration recovery (automatic)
// If server responds with 403, middleware:
//   - Detects the error
//   - Returns to phase 1 (fresh token fetch with new cookies)
//   - Retries the original request
// All automatic—no retry logic in application code
```

## Stateful API Patterns

### Login Session

```go
type LoginRequest struct {
    Username string `json:"username"`
    Password string `json:"password"`
}

type LoginResponse struct {
    Token string `json:"token"`
    User  string `json:"user"`
}

// Client maintains session cookies across requests
client := relay.New(relay.WithBaseURL("https://api.example.com"))

// Step 1: Login - server sends Set-Cookie with session ID
loginResp := LoginResponse{}
_, err := relay.Post[LoginRequest, LoginResponse](
    ctx, client,
    "/auth/login",
    LoginRequest{Username: "user", Password: "pass"},
    &loginResp,
)

// Step 2: Access protected resource
// Session cookie is automatically included
userData := map[string]interface{}{}
_, err = relay.Get[map[string]interface{}](ctx, client, "/api/user", &userData)
// No need to manually add Cookie header or pass tokens

// Step 3: Logout
_, err = relay.Post[struct{}, struct{}](ctx, client, "/auth/logout", struct{}{}, nil)
```

### Multi-step OAuth2

```go
// Step 1: Get authorization code (server redirects with code)
// Server may set session cookies
_, _ = relay.Get[Response](ctx, client, "/oauth/authorize?...", &resp)

// Step 2: Exchange code for token
// Session cookies are maintained
tokenResp := TokenResponse{}
_, _ = relay.Post[...](ctx, client, "/oauth/token", ..., &tokenResp)

// Step 3: Use token with stored session cookies
// Both are maintained transparently
_, _ = relay.Get[...](ctx, client, "/api/protected", ...)
```

## Cookie Inspection

Access stored cookies programmatically:

```go
import (
    "net/url"
    "github.com/jhonsferg/relay"
)

client := relay.New(relay.WithBaseURL("https://api.example.com"))

// After making requests that set cookies
// ... make requests ...

// Inspect cookies (if http.CookieJar is stored)
baseURL, _ := url.Parse("https://api.example.com")
cookies := client.CookieJar.Cookies(baseURL)

for _, cookie := range cookies {
    log.Printf("Cookie: %s=%s (expires: %v)", cookie.Name, cookie.Value, cookie.Expires)
}
```

## Cookie Behavior

### Domain & Path Scoping

Cookies are automatically scoped by domain and path per RFC 6265:

```go
// Cookie set for https://api.example.com/auth/
// is included in requests to https://api.example.com/auth/login
// but NOT in requests to https://api.example.com/payment/

// Cookie set with Domain=.example.com
// is included in requests to sub.example.com, api.example.com, etc.
```

### Secure & HttpOnly

Cookies are automatically managed with:

- **Secure flag** - only sent over HTTPS (no plain HTTP)
- **HttpOnly flag** - not accessible to JavaScript (server-side only)
- **Expiration** - automatically removed when expired
- **SameSite** - automatically enforced per RFC 6265bis

### Cookie Jar Persistence

The default `http.CookieJar` is in-memory only:

```go
// Cookies are lost when client is garbage collected
client1 := relay.New(...)
// make requests, cookies stored in client1
// ...

client2 := relay.New(...)
// NEW client, no cookies from client1
// This is expected behavior—cookies are not persistent across process restarts
```

For persistent cookies, implement a custom `http.CookieJar`:

```go
import (
    "encoding/json"
    "net/http"
    "os"
)

type PersistentJar struct {
    underlying http.CookieJar
    path       string
}

func (pj *PersistentJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
    pj.underlying.SetCookies(u, cookies)
    pj.persist()
}

func (pj *PersistentJar) Cookies(u *url.URL) []*http.Cookie {
    return pj.underlying.Cookies(u)
}

func (pj *PersistentJar) persist() {
    // Save to disk...
}

client := relay.New(relay.WithCookieJar(&PersistentJar{...}))
```

## Troubleshooting

### Cookies Not Being Sent

If cookies aren't being sent to the server:

1. **Verify domain match** - Cookie domain must match request URL
   ```go
   // Cookie: Domain=api.example.com
   // Request: https://api.example.com ✓
   // Request: https://example.com ✗
   ```

2. **Check Secure flag** - HTTPS-only cookies won't work over HTTP
   ```go
   // Server: Set-Cookie: token=abc; Secure
   // HTTP request won't include cookie
   // HTTPS request will include cookie
   ```

3. **Inspect cookies**
   ```go
   cookies := client.CookieJar.Cookies(baseURL)
   log.Printf("Cookies: %v", cookies)
   ```

### Cookies Being Lost

If cookies aren't persisting:

1. **Different client instance** - Create one client, reuse it
   ```go
   // WRONG
   for i := 0; i < 10; i++ {
       c := relay.New(...) // New jar every iteration!
       // ...
   }
   
   // RIGHT
   client := relay.New(...)
   for i := 0; i < 10; i++ {
       // Reuse client
       // ...
   }
   ```

2. **Cookie expiration** - Check `Set-Cookie` response headers
   ```go
   // Set-Cookie: session=xyz; Max-Age=3600
   // Cookie expires after 3600 seconds
   ```

## See also

- [Authentication Guide](auth.md)
- [Hooks & Middleware](hooks.md)
- [traverse SAP CSRF Flow](https://jhonsferg.github.io/traverse/guides/sap/)
- [CHANGELOG v0.4.0](../changelog.md#040-2026-04-27)
