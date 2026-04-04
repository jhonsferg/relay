# relay-bench

A load-testing tool for HTTP endpoints, powered by the [relay](../../README.md) library.  
Supports both **fixed-count** and **duration-based** modes, concurrent workers, and produces detailed latency statistics including percentiles.

---

## Building

### Prerequisites

- [Go 1.22+](https://go.dev/dl/)

### Linux / macOS

```bash
cd relay
go build -o relay-bench ./cmd/relay-bench

# Or install globally
go install github.com/jhonsferg/relay/cmd/relay-bench@latest
```

### Windows (PowerShell)

```powershell
cd relay
go build -o relay-bench.exe .\cmd\relay-bench

go install github.com/jhonsferg/relay/cmd/relay-bench@latest
```

### Cross-compile

```bash
# Windows x64
GOOS=windows GOARCH=amd64 go build -o relay-bench.exe ./cmd/relay-bench

# macOS ARM64
GOOS=darwin GOARCH=arm64 go build -o relay-bench-darwin-arm64 ./cmd/relay-bench

# Linux ARM64
GOOS=linux GOARCH=arm64 go build -o relay-bench-linux-arm64 ./cmd/relay-bench
```

---

## Usage

```
relay-bench [OPTIONS] <URL>
```

### Quick start

```bash
# 100 sequential requests
relay-bench -n 100 https://httpbin.org/get

# 30 seconds, 10 concurrent workers
relay-bench -d 30s -c 10 https://httpbin.org/get

# POST with JSON body, 50 RPS, 20 workers
relay-bench -m POST -j '{"ping":true}' --rate 50 -c 20 -d 60s https://api.example.com/endpoint
```

---

## Options reference

| Flag | Default | Description |
|------|---------|-------------|
| `-n count` | `100` | Total number of requests (ignored when `-d` is set) |
| `-d duration` | | Run for this duration instead of a fixed count (e.g. `30s`, `5m`) |
| `-c workers` | `1` | Number of concurrent workers |
| `-m method` | `GET` | HTTP method |
| `-H "K: V"` | | Add request header (repeatable) |
| `-j json` | | JSON request body (sets `Content-Type: application/json`) |
| `-b body` | | Raw request body |
| `--timeout` | `10s` | Per-request timeout |
| `--retry n` | `0` | Maximum retry attempts per request |
| `--rate rps` | `0` | Requests per second limit across all workers (0 = unlimited) |
| `--cb` | | Enable circuit breaker |
| `--cb-failures n` | `5` | Consecutive failures before circuit opens |
| `--json` | | Output results as JSON instead of a text table |
| `--version` | | Print version and exit |

---

## Examples

### Fixed count

```bash
# 500 requests, 25 workers
relay-bench -n 500 -c 25 https://api.example.com/items

# POST JSON, 200 requests
relay-bench -n 200 -m POST -j '{"user":"test"}' https://api.example.com/users
```

### Duration mode

```bash
# Hammer an endpoint for 2 minutes with 50 workers
relay-bench -d 2m -c 50 https://api.example.com/search

# Rate-limited: max 100 RPS for 1 minute
relay-bench -d 1m -c 20 --rate 100 https://api.example.com/ratelimited
```

### JSON output (for scripting / CI)

```bash
relay-bench -n 1000 -c 10 --json https://api.example.com/health | jq '.p99_ms'
```

---

## Output

### Text format (default)

```
Requests:       1000  (total)
Duration:       4.231s
Throughput:     236.4 req/s

Latency:
  Min           2.1 ms
  Mean          42.3 ms
  Median        38.7 ms
  P90           81.2 ms
  P95           97.4 ms
  P99          143.8 ms
  Max          312.5 ms

Status codes:
  200           987
  500            13
Errors:           0
Failures:        13
```

### JSON format (`--json`)

```json
{
  "total":       1000,
  "duration_ms": 4231,
  "rps":         236.4,
  "min_ms":      2.1,
  "mean_ms":     42.3,
  "median_ms":   38.7,
  "p90_ms":      81.2,
  "p95_ms":      97.4,
  "p99_ms":      143.8,
  "max_ms":      312.5,
  "errors":      0,
  "failures":    13,
  "status_codes": {"200": 987, "500": 13}
}
```

---

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | All requests succeeded |
| `1` | Usage error, network failure, or at least one request failed |
