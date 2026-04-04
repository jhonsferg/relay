# relay-probe

A health-monitoring tool for HTTP endpoints, powered by the [relay](../../README.md) library.  
Checks one or more URLs against configurable expectations (status code, body content, response time) and optionally watches them on a recurring interval — ideal for readiness checks, smoke tests, and uptime monitoring.

---

## Building

### Prerequisites

- [Go 1.22+](https://go.dev/dl/)

### Linux / macOS

```bash
cd relay
go build -o relay-probe ./cmd/relay-probe

# Or install globally
go install github.com/jhonsferg/relay/cmd/relay-probe@latest
```

### Windows (PowerShell)

```powershell
cd relay
go build -o relay-probe.exe .\cmd\relay-probe

go install github.com/jhonsferg/relay/cmd/relay-probe@latest
```

### Cross-compile

```bash
# Windows x64
GOOS=windows GOARCH=amd64 go build -o relay-probe.exe ./cmd/relay-probe

# macOS ARM64
GOOS=darwin GOARCH=arm64 go build -o relay-probe-darwin-arm64 ./cmd/relay-probe

# Linux ARM64
GOOS=linux GOARCH=arm64 go build -o relay-probe-linux-arm64 ./cmd/relay-probe
```

---

## Usage

```
relay-probe [OPTIONS] <URL> [URL...]
```

### Quick start

```bash
# Check a single endpoint
relay-probe https://httpbin.org/get

# Expect HTTP 201 and a specific string in the body
relay-probe --expect 201 --contains '"created":true' https://api.example.com/resource

# Watch all endpoints every 15 seconds
relay-probe --watch --interval 15s \
  https://api.example.com/health \
  https://api.example.com/readiness \
  https://api.example.com/liveness
```

---

## Options reference

| Flag | Default | Description |
|------|---------|-------------|
| `--expect code` | `200` | Expected HTTP status code |
| `--contains text` | | Assert that the response body contains this string |
| `--max-latency` | `0` | Fail if response time exceeds this duration (e.g. `500ms`, `2s`; 0 = disabled) |
| `-H "K: V"` | | Add request header (repeatable) |
| `-u user:pass` | | HTTP Basic authentication |
| `-t token` | | Bearer token |
| `--timeout` | `10s` | Per-request timeout |
| `--retry n` | `0` | Maximum retry attempts on failure |
| `--watch` | | Continuously probe on a recurring interval |
| `--interval` | `30s` | Polling interval in watch mode |
| `--json` | | Output results as JSON |
| `--version` | | Print version and exit |

---

## Examples

### One-shot checks

```bash
# Check multiple endpoints at once
relay-probe \
  https://api.example.com/health \
  https://api.example.com/metrics

# Strict check: expect 200, body must contain "ok", latency under 300 ms
relay-probe --expect 200 --contains '"status":"ok"' --max-latency 300ms \
  https://api.example.com/health

# Authenticated check
relay-probe -t $TOKEN --expect 200 https://api.example.com/me

# Check behind basic auth
relay-probe -u admin:secret https://internal.example.com/admin/health
```

### Watch mode (continuous monitoring)

```bash
# Poll every 10 seconds, fail if any endpoint goes down
relay-probe --watch --interval 10s \
  https://service-a.example.com/health \
  https://service-b.example.com/health

# Watch with strict latency budget
relay-probe --watch --interval 30s --max-latency 1s \
  https://api.example.com/health
```

### CI / scripting

```bash
# Use exit code in a CI pipeline
relay-probe --expect 200 https://staging.example.com/health
if [ $? -ne 0 ]; then
  echo "Staging health check failed — aborting deployment"
  exit 1
fi

# JSON output for structured logging
relay-probe --json https://api.example.com/health | jq '.results[0].latency_ms'
```

---

## Output

### Text format (default)

```
  ✓  https://api.example.com/health       200  OK        45ms
  ✓  https://api.example.com/readiness    200  OK        38ms
  ✗  https://api.example.com/liveness     503  Unhealthy  12ms  body missing "ok"

1 of 3 checks failed.
```

### JSON format (`--json`)

```json
{
  "results": [
    {
      "url":        "https://api.example.com/health",
      "ok":         true,
      "status":     200,
      "latency_ms": 45,
      "error":      ""
    },
    {
      "url":        "https://api.example.com/liveness",
      "ok":         false,
      "status":     503,
      "latency_ms": 12,
      "error":      "body missing expected string"
    }
  ]
}
```

---

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | All checks passed |
| `1` | One or more checks failed |
| `2` | Usage error or configuration problem |
