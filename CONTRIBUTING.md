# Contributing to relay

Thank you for your interest in contributing! This guide will get you set up quickly.

## Setup

```bash
git clone https://github.com/jhonsferg/relay.git
cd relay
go mod download
go test ./...
```

## Running Tests

```bash
# All tests
go test ./relay/... ./testutil/...

# With race detector (requires CGO)
CGO_ENABLED=1 go test -race ./relay/... ./testutil/...

# With coverage report
go test -coverprofile=coverage.out ./relay/...
go tool cover -html=coverage.out
```

## Code Style

- Run `gofmt -w .` before committing
- Run `go vet ./...` to catch common issues
- Run `golangci-lint run ./...` if you have it installed
- Follow the existing code patterns (fluent builder style, functional options)

## Commit Convention

This project follows [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add WebSocket upgrade support
fix: correct ETag header comparison (case-insensitive)
docs: add Redis cache store example
test: increase circuit breaker test coverage
refactor: extract backoff logic to internal/backoff
chore: bump go.opentelemetry.io/otel to v1.25.0
```

## Pull Request Process

1. Fork the repository and create a feature branch from `main`
2. Make your changes with tests covering the new behavior
3. Update `CHANGELOG.md` under `[Unreleased]`
4. Ensure `go test ./...` and `go vet ./...` pass
5. Open a PR with a clear description of what it does and why

## Reporting Security Vulnerabilities

Please **do not** open a public issue for security vulnerabilities.
See [SECURITY.md](SECURITY.md) for the responsible disclosure process.

## Code of Conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md).
All contributors are expected to uphold its standards.
