# relay CLI

A feature-rich HTTP client for the command line, powered by the [relay](../../README.md) library.  
Inspired by **curl** and **httpie**, it exposes relay's retry, circuit-breaker, rate-limiting, streaming, download/upload and timing capabilities directly from your terminal.

---

## Building

### Prerequisites

- [Go 1.22+](https://go.dev/dl/)
- The workspace root (`relay/`) must be your working directory when using `go work`.

### Linux / macOS

```bash
# From the workspace root
cd relay
go build -o relay ./cmd/relay

# Or install to $GOPATH/bin
go install github.com/jhonsferg/relay/cmd/relay@latest
```

### Windows (PowerShell)

```powershell
cd relay
go build -o relay.exe .\cmd\relay

# Or install to %GOPATH%\bin
go install github.com/jhonsferg/relay/cmd/relay@latest
```

### Cross-compile

```bash
# Windows x64 binary from Linux/macOS
GOOS=windows GOARCH=amd64 go build -o relay.exe ./cmd/relay

# macOS ARM64 (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o relay-darwin-arm64 ./cmd/relay

# Linux ARM64
GOOS=linux GOARCH=arm64 go build -o relay-linux-arm64 ./cmd/relay
```

---

## Usage

```
relay [OPTIONS] <URL> [URL...]
```

### Quick start

```bash
# Simple GET
relay https://httpbin.org/get

# Pretty-print JSON response
relay --pretty https://httpbin.org/get

# POST with JSON body
relay -X POST -j '{"name":"Alice"}' https://httpbin.org/post

# Show per-phase timing
relay --timing https://httpbin.org/get
```

---

## Options reference

### Request

| Flag | Default | Description |
|------|---------|-------------|
| `-X method` | `GET` | HTTP method (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS) |
| `-d body` | | Request body. Prefix with `@file` to read from a file, `@-` to read from stdin |
| `-j json` | | JSON body - sets `Content-Type: application/json` automatically |
| `-H "K: V"` | | Add request header (repeatable) |
| `-q "k=v"` | | Add query parameter (repeatable) |
| `-F "k=v"` | | Add multipart form field (repeatable) |
| `-A string` | | Custom `User-Agent` |

### Authentication

| Flag | Description |
|------|-------------|
| `-u user:pass` | HTTP Basic authentication |
| `-t token` | Bearer token (`Authorization: Bearer <token>`) |
| `-k Header=value` | Arbitrary API-key header (e.g. `-k X-API-Key=secret`) |
| `-b "Name=Val"` | Send cookies as a string |
| `-c file` | Netscape cookie jar - loads cookies on start, saves on exit |

### Network

| Flag | Default | Description |
|------|---------|-------------|
| `--timeout` | `0` | Max total transfer duration - **0 means no limit** (Ctrl+C always cancels). Set e.g. `--timeout 60s` for API calls where you want a hard deadline. |
| `--connect-timeout` | `30s` | TCP/TLS connection timeout. Does **not** affect body transfer time. |
| `-L n` | `10` | Maximum redirects (0 disables redirects) |
| `--proxy url` | | HTTP/HTTPS proxy URL |
| `--insecure` | | Skip TLS certificate verification |

### Retry & resilience

| Flag | Default | Description |
|------|---------|-------------|
| `--retry n` | `0` | Maximum retry attempts |
| `--retry-delay` | `100ms` | Initial retry interval (doubles each attempt) |
| `--retry-verbose` | | Print each retry attempt to stderr |
| `--rate rps` | `0` | Requests per second limit (0 = unlimited) |
| `--cb` | | Enable circuit breaker |
| `--cb-failures n` | `5` | Consecutive failures before circuit opens |

### Output & download

| Flag | Description |
|------|-------------|
| `-o file` | Write response body to file |
| `-O` | Save with the remote filename (from `Content-Disposition` or URL path) |
| `-C` | Resume a partial download (`-o` or `-O` required) |
| `-P n` | Maximum parallel downloads when multiple URLs are provided |
| `--upload-file file` | Upload a file via PUT with a live progress bar |
| `-D file` | Write response headers to file (HTTP/1.1 format) |
| `-I` | Perform a HEAD request and print headers |
| `--pretty` | Pretty-print JSON response body |
| `-s` | Silent - suppress all output (exit code reflects HTTP status) |
| `-i` | Include response headers in stdout output |
| `-v` | Verbose - print request/response headers to stderr |
| `--timing` | Print per-phase timing breakdown to stderr |
| `--no-progress` | Disable download/upload progress bar |
| `--version` | Print version and exit |

---

## Examples

### Basic requests

```bash
# GET with custom header
relay -H "Accept: application/json" https://httpbin.org/get

# POST form data
relay -F "username=alice" -F "email=alice@example.com" https://httpbin.org/post

# DELETE with bearer token
relay -X DELETE -t mysecrettoken https://api.example.com/items/42

# Read body from stdin
echo '{"event":"click"}' | relay -X POST -d @- https://api.example.com/events
```

### Downloads

```bash
# Download with progress bar, auto-named from URL
relay -O https://example.com/archive.tar.gz

# Download with explicit output filename
relay -o myfile.zip https://example.com/file.zip

# Resume a partial download
relay -O -C https://example.com/large-file.iso

# Download 4 files in parallel (max 2 at a time)
relay -O -P 2 \
  https://cdn.example.com/a.zip \
  https://cdn.example.com/b.zip \
  https://cdn.example.com/c.zip \
  https://cdn.example.com/d.zip
```

### Upload

```bash
# Upload a file via PUT with live progress bar
relay --upload-file firmware.bin https://api.example.com/upload
```

### Resilience

```bash
# Retry up to 3 times with verbose output
relay --retry 3 --retry-verbose https://api.example.com/flaky

# Rate-limited requests with circuit breaker
relay --rate 50 --cb --cb-failures 3 https://api.example.com/endpoint
```

### Cookies

```bash
# Login and save cookies to jar
relay -c cookies.txt -X POST -j '{"user":"alice","pass":"secret"}' https://example.com/login

# Use saved cookies for subsequent requests
relay -c cookies.txt https://example.com/dashboard
```

---

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success (2xx/3xx) |
| `1` | Usage error or network failure |
| `4` | HTTP 4xx client error |
| `5` | HTTP 5xx server error |
