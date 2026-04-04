# Git Workflow — relay

Engineering standards and Git workflow for the relay project.
This document is the authoritative reference for contributors and automated agents.

---

## Table of Contents

1. [Commit rules](#1-commit-rules)
2. [Branch naming](#2-branch-naming)
3. [Git workflow](#3-git-workflow)
4. [GitHub CLI reference](#4-github-cli-reference)
5. [Versioning and releases](#5-versioning-and-releases)
6. [Linting](#6-linting)
7. [Code documentation](#7-code-documentation)
8. [Code coverage](#8-code-coverage)
9. [Testing rules](#9-testing-rules)
10. [Performance rules](#10-performance-rules)
11. [API stability](#11-api-stability)
12. [Memory safety](#12-memory-safety)
13. [Security](#13-security)
14. [Typography and style](#14-typography-and-style)
15. [Extension modules](#15-extension-modules)
16. [Makefile targets](#16-makefile-targets)

---

## 1. Commit Rules

- No Co-Authored-By, no Signed-off-by, no author emails in commits — ever.
- Subject line 72 characters maximum, imperative mood, no trailing period.
- Format: `type(scope): description`

### Commit types

| Type | When to use |
|---|---|
| `feat` | New user-facing feature or public API addition |
| `fix` | Bug fix (production code) |
| `perf` | Performance improvement with benchmark evidence |
| `refactor` | Internal restructuring, no behaviour change |
| `test` | Adding or correcting tests only |
| `bench` | Benchmark additions or updates |
| `docs` | Documentation, comments, README only |
| `chore` | Dependency updates, tooling, config files |
| `ci` | GitHub Actions workflows, CI scripts |
| `security` | Security fixes, vulnerability patches |
| `build` | Build system, Makefile, Dockerfile changes |

### Commit scope

The scope is optional. Use the package name when applicable.

Examples: `(client)`, `(retry)`, `(cache)`, `(ext/redis)`, `(ci)`, `(docs)`

### Commit body

- Add a blank line after the subject, then write the body.
- Use the body only when the *why* is non-obvious.
- Reference issues with `Closes #N` or `Refs #N` at the end of the body.
- Wrap body lines at 72 characters.

### Examples

```
feat(client): add WithBulkhead option for concurrency limiting

Adds semaphore-based concurrency control per client instance.
Complements the existing rate limiter (frequency) with a cap on
simultaneous in-flight requests.

Closes #42
```

```
fix(retry): prevent duplicate idempotency key on manual retry
```

```
perf(cache): replace linear scan with hash lookup in inMemoryStore

BenchmarkCacheGet: 312 ns/op -> 87 ns/op (-72%)
```

---

## 2. Branch Naming

All branches follow the pattern `type/short-description`.
Use lowercase and hyphens only (no underscores, no extra slashes).

| Prefix | Purpose | Example |
|---|---|---|
| `feat/` | New feature | `feat/bulkhead-isolation` |
| `fix/` | Bug fix | `fix/nil-pointer-response` |
| `perf/` | Performance work | `perf/dns-cache-warmup` |
| `refactor/` | Internal restructuring | `refactor/request-builder` |
| `test/` | Tests only | `test/coverage-client` |
| `coverage/` | Coverage boost | `coverage/boost-to-88` |
| `docs/` | Documentation only | `docs/update-readme` |
| `chore/` | Maintenance, deps | `chore/update-dependencies` |
| `ci/` | CI/CD changes | `ci/fix-autotag-bash` |
| `security/` | Security fixes | `security/patch-cve-2025-xxxx` |
| `hotfix/` | Urgent production fix | `hotfix/nil-pointer-response` |
| `release/` | Release preparation | `release/v0.2.0` |
| `optimize/` | Micro-optimisation | `optimize/zero-alloc-retry` |
| `ext/` | Extension module work | `ext/add-http3-transport` |

Rules:

- Never commit directly to `master`.
- One branch per logical change — do not bundle unrelated work.
- Delete remote branches after the PR is merged.

---

## 3. Git Workflow

This project uses a simplified GitFlow adapted for a single-maintainer library.

```
master ---- always releasable, protected, tagged -> auto-release
  |
  |-- feat/...      --> PR --> squash-merge --> master
  |-- fix/...       --> PR --> squash-merge --> master
  |-- hotfix/...    --> PR --> merge (no squash) --> master + tag immediately
  `-- release/...   --> final changelog edit --> PR --> merge --> tag
```

### Standard flow for any change

```bash
# 1. Create branch from latest master
git fetch origin && git checkout -b feat/my-feature origin/master

# 2. Make atomic commits (follow commit rules above)
git add <files>
git commit -m "feat(scope): short description"

# 3. Run the local quality gate
make all   # runs fmt + lint + tidy + test

# 4. Push and open PR
git push origin feat/my-feature
gh pr create --repo <your-github-user>/relay --base master \
  --title "feat(scope): short description" \
  --body "..."

# 5. Wait for all CI checks to pass, then merge
gh pr merge <number> --repo <your-github-user>/relay --squash --delete-branch
```

### Hotfix flow

```bash
git checkout -b hotfix/critical-bug origin/master
# fix, commit
git push origin hotfix/critical-bug
gh pr create --repo <your-github-user>/relay --base master --title "fix: ..."
# after merge, tag immediately
gh release create v0.X.Y --repo <your-github-user>/relay --notes "fix: ..."
```

---

## 4. GitHub CLI Reference

### Authentication

```bash
# Verify authentication
gh auth status

# Login if needed
gh auth login --hostname github.com --git-protocol ssh
```

### SSH configuration for this repository

```bash
export GIT_SSH_COMMAND="ssh -i ~/.ssh/github_<your-key-name> -o StrictHostKeyChecking=no"
```

### Branch protection bypass (direct push to master)

Only for emergency fixes or history rewrites. Re-enable immediately after.

```bash
# Disable enforce_admins
gh api -X DELETE repos/<your-github-user>/relay/branches/master/protection/enforce_admins

# Push
git push origin master

# Re-enable
gh api -X POST repos/<your-github-user>/relay/branches/master/protection/enforce_admins
```

To allow force pushes (e.g. for history rewrite with git-filter-repo):

```bash
gh api -X PUT repos/<your-github-user>/relay/branches/master/protection \
  --input - <<'EOF'
{
  "required_status_checks": null,
  "enforce_admins": false,
  "required_pull_request_reviews": null,
  "restrictions": null,
  "allow_force_pushes": true
}
EOF

git push origin master --force

# Restore protection
gh api -X PUT repos/<your-github-user>/relay/branches/master/protection \
  --input - <<'EOF'
{
  "required_status_checks": null,
  "enforce_admins": true,
  "required_pull_request_reviews": {
    "required_approving_review_count": 1,
    "dismiss_stale_reviews": false
  },
  "restrictions": null,
  "allow_force_pushes": false,
  "required_linear_history": true,
  "required_conversation_resolution": true
}
EOF
```

### PR management

```bash
# Create PR
gh pr create --repo <your-github-user>/relay \
  --head feat/my-feature --base master \
  --title "feat(scope): description" \
  --body "..."

# List open PRs
gh pr list --repo <your-github-user>/relay --state open

# View PR checks and status
gh pr view 42 --repo <your-github-user>/relay

# Merge (squash, delete branch)
gh pr merge 42 --repo <your-github-user>/relay \
  --squash --delete-branch --subject "feat(scope): description"

# Merge as admin (bypass required reviews)
gh pr merge 42 --admin --squash --delete-branch
```

### Release management

```bash
# Tag triggers goreleaser automatically via release.yml
git tag v0.2.0 && git push origin v0.2.0

# Create a lightweight release manually
gh release create v0.2.0 \
  --repo <your-github-user>/relay \
  --title "v0.2.0" \
  --notes "## Changes..."

# List releases
gh release list --repo <your-github-user>/relay

# View a release
gh release view v0.2.0 --repo <your-github-user>/relay
```

### Issue management

```bash
# Create issue
gh issue create --repo <your-github-user>/relay \
  --title "bug: ..." --body "..." --label "bug"

# List open issues
gh issue list --repo <your-github-user>/relay --state open

# Close issue
gh issue close 15 --repo <your-github-user>/relay
```

### CI/CD inspection

```bash
# List recent workflow runs
gh run list --repo <your-github-user>/relay --limit 10

# Watch a run live
gh run watch --repo <your-github-user>/relay

# View failed job logs
gh run view <run-id> --repo <your-github-user>/relay --log-failed

# Re-run only failed jobs
gh run rerun <run-id> --repo <your-github-user>/relay --failed
```

---

## 5. Versioning and Releases

This project follows [Semantic Versioning 2.0](https://semver.org).

### Version bump rules

| Change | Bump | Example |
|---|---|---|
| Breaking API change | MAJOR | `v1.0.0 -> v2.0.0` |
| New backward-compatible feature | MINOR | `v0.1.0 -> v0.2.0` |
| Backward-compatible bug fix | PATCH | `v0.1.0 -> v0.1.1` |
| Urgent security fix | PATCH | `v0.1.0 -> v0.1.1` |

### Pre-release versions

- Alpha: `v0.2.0-alpha.1`
- Beta: `v0.2.0-beta.1`
- Release candidate: `v0.2.0-rc.1`

### Release process

```bash
# 1. Ensure all CI checks pass on master
gh run list --repo <your-github-user>/relay --branch master --limit 3

# 2. Update CHANGELOG.md with changes since last tag
git log v0.1.12..HEAD --oneline --no-merges

# 3. Commit changelog
git commit -m "docs(changelog): update for v0.2.0"

# 4. Push tag - release.yml triggers goreleaser automatically
git tag v0.2.0
git push origin v0.2.0

# 5. Verify
gh release view v0.2.0 --repo <your-github-user>/relay
```

### Extension module versioning

Each `ext/*` module has its own `go.mod` and version tag:

- Tag format: `ext/redis/v0.1.0`, `ext/tracing/v0.1.0`
- Bump independently when the ext module changes.
- Always update the ext `go.mod` to reference the latest core relay version before tagging.

### CHANGELOG.md format

```markdown
## [v0.2.0] - 2026-MM-DD

### Added
- feat(client): WithBulkhead option for concurrency limiting

### Fixed
- fix(retry): prevent duplicate idempotency key on retry

### Performance
- perf(cache): replace linear scan with hash lookup (-72% ns/op)
```

---

## 6. Linting

Config file: `.golangci.yml` (version `"2"` is mandatory in the file header).

### Running the linter

```bash
# Full project
golangci-lint run ./...

# Single package
golangci-lint run ./cache/...

# With auto-fix where possible
golangci-lint run --fix ./...

# Extension modules (each has its own go.mod)
find ext -name "go.mod" | while read f; do
  dir=$(dirname "$f")
  (cd "$dir" && GOWORK=off golangci-lint run ./...)
done
```

### Fixing common issues

| Issue | Fix |
|---|---|
| `gofmt` formatting | `gofmt -s -w <file>` |
| `goimports` import ordering | `goimports -w <file>` |
| `errcheck`: unchecked error | Handle the error or explicitly discard with `_` |
| `unused`: dead code | Remove it or suppress with `//nolint:unused // reason` |
| `ineffassign` | Remove the redundant assignment |
| `misspell` | Fix the spelling (UK locale: colour, behaviour, licence) |
| `gosec` false positive | `//nolint:gosec // reason` on that line only |
| `shadow` variable | Rename the inner variable to avoid shadowing |

### Inline suppression

Use sparingly. Always include a reason.

```go
//nolint:gosec // SHA-1 is used for cache key hashing, not cryptography
h := sha1.New()

result, _ := doSomething() //nolint:errcheck // documented: always returns nil
```

### golangci-lint v2 constraints

- Formatters (`gofmt`, `goimports`) go under `formatters:` section, not `linters:`.
- `gosimple` and `typecheck` do not exist in v2 — do not add them.
- `exclude-dirs` does not exist at the top level or under `issues:`.
- `misspell` locale must be `UK`.

### Pre-commit hooks

The project uses [lefthook](https://github.com/evilmartians/lefthook). Install once:

```bash
go install github.com/evilmartians/lefthook@latest
lefthook install
```

Hooks run `gofmt` and `golangci-lint` automatically before every commit.

---

## 7. Code Documentation

### Godoc rules (mandatory for all exported symbols)

- Every exported type, function, method, constant, and variable must have a doc comment.
- Start comments with the symbol name: `// FunctionName does X.`
- Use complete sentences ending with a period.
- Describe *what* and *why*, not *how* — the code shows how.

### Package-level documentation

Each package must have a `doc.go` file:

```go
// Package cache provides an in-memory HTTP response cache implementing
// RFC 7234 cache semantics. It plugs into the relay transport stack
// via the CacheStore interface.
package cache
```

### Function and method comments

```go
// WithBulkhead returns an Option that limits the number of concurrent
// in-flight requests to maxConcurrent. When a request cannot acquire
// a slot within the context deadline, ErrBulkheadFull is returned.
// Pass 0 to disable the bulkhead.
func WithBulkhead(maxConcurrent int) Option {
```

### Interface documentation

```go
// CacheStore is the storage backend for HTTP response caching. All
// methods must be safe for concurrent use from multiple goroutines.
type CacheStore interface {
    // Get returns the cached entry for key. Returns false if the entry
    // is not present or has expired.
    Get(key string) (*CachedResponse, bool)

    // Set stores the entry under key. Implementations may evict entries
    // to respect capacity constraints.
    Set(key string, entry *CachedResponse)
}
```

### Inline comments

- Comment non-obvious logic, not self-evident code.
- Use `// NOTE:` for important observations.
- Use `// TODO: #issue-number` for known gaps — always link to an issue.
- Use `// FIXME:` only for bugs that need a follow-up PR — never merge FIXME.
- Avoid block comments (`/* */`) in Go source.

### Testable examples

Place in `example_test.go` files:

```go
func ExampleClient_Execute() {
    client, _ := relay.New(relay.WithBaseURL("https://api.example.com"))
    resp, _ := client.Execute(client.Get("/users"))
    fmt.Println(resp.StatusCode)
    // Output: 200
}
```

---

## 8. Code Coverage

### Thresholds

| Scope | Minimum |
|---|---|
| Core library (`relay` package) | 85% |
| Extension modules (`ext/*`) | 75% |

### Measuring coverage locally

```bash
# Generate coverage profile
go test -coverprofile=coverage.out -covermode=atomic ./...

# Filter out non-library packages (matches CI behaviour)
grep -v -E "^github\.com/<your-github-user>/relay/(cmd|examples|benchmarks)/" \
  coverage.out > coverage_lib.out

# View total percentage
go tool cover -func=coverage_lib.out | grep '^total'

# Open HTML report
go tool cover -html=coverage_lib.out -o coverage.html
```

### Packages excluded from coverage

These packages have intentionally 0% and must not inflate the denominator:

- `cmd/**` — binary entrypoints, not library code
- `examples/**` — illustrative examples
- `benchmarks/**` — benchmark-only code

Configure in `codecov.yml`:

```yaml
ignore:
  - "cmd/**"
  - "examples/**"
  - "benchmarks/**"
```

### Coverage-complete test patterns

- Test all exported functions including error paths.
- Test boundary conditions: nil input, empty slice, zero value, maximum value.
- Use `testutil/recorder` for HTTP-level tests — no live network calls in unit tests.
- Use `go test -race ./...` when writing any concurrent code.

---

## 9. Testing Rules

- Every change must pass `go test ./...` locally before pushing.
- Run `go test -race ./...` before opening any PR.
- Any data race is a release blocker, not a warning.
- Table-driven tests are preferred for functions with multiple input variants.
- Test file naming: `<file>_test.go` in the same package; use `_test` package suffix for black-box public API tests.
- Do not use `time.Sleep` in tests — use channels, `sync.WaitGroup`, or mock clocks.

### Known concurrency trap

Never pool `httptrace.ClientTrace` — the transport's `dialParallel` fires
callbacks from background goroutines after `Do()` returns. Use `atomic.Int64`
for any field written by trace callbacks (see `timing.go`).

### Test helpers

```go
// Use the mock recorder for HTTP-level tests
rec := testutil.NewRecorder()
rec.Enqueue(200, `{"id":1}`)
client, _ := relay.New(relay.WithBaseURL(rec.URL()))
```

---

## 10. Performance Rules

- Every optimisation must show a measurable improvement greater than 5%.
- Measure with `go test -bench=. -benchmem -count=6` before and after each change.
- Use `benchstat before.txt after.txt` to validate; attach output in the commit body.
- Do not optimise unmeasured code. No speculative micro-optimisations.
- Profile before optimising: `go test -cpuprofile=cpu.prof -bench=. && go tool pprof cpu.prof`

---

## 11. API Stability

- Zero breaking changes to exported types, functions, or interfaces without a MAJOR version bump.
- Public `Response` fields are stable: `StatusCode`, `Status`, `Headers`, `Timing`,
  `Truncated`, `RedirectCount`, `Body()`, `String()`.
- New options use the functional options pattern: `WithXxx(...) Option`.
- New behaviour is always opt-in via options — never change default behaviour.
- Deprecate before removing: add `// Deprecated: use Xxx instead.` and keep for one minor version.
- `PutResponse(r)` is opt-in; callers who do not call it are safe (no use-after-free).

---

## 12. Memory Safety

- Pooled buffers from `pool.GetSizedBuffer` must be returned via `pool.PutSizedBuffer`
  before handing the `Response` back to the caller — the caller owns the body slice.
- `bytes.Reader` from `pool.GetBytesReader` must be released in `releasePooledReader()`,
  called after every `RoundTrip` attempt (success or failure).
- Never return a slice that aliases a pool buffer to the caller.
- Never store a pointer to a pooled object beyond the scope of the function that acquired it.

---

## 13. Security

- Run `govulncheck ./...` before any release tag.
- Never commit secrets, tokens, or credentials — use environment variables or `gh secret set`.
- TLS minimum version is `tls.VersionTLS12` (enforced via Trivy and CodeQL).
- `gosec` linter is enabled; suppressions require a written justification comment.
- Dependency updates: run `go get -u ./... && go mod tidy` monthly and open a `chore/` PR.

---

## 14. Typography and Style

- UK English throughout: `colour`, `behaviour`, `licence`, `initialise`, `optimise`.
- Never use the em-dash symbol. Use a plain hyphen ( - ) instead.
- No trailing whitespace in any file.
- Files end with a single newline character.
- Maximum line length for comments and documentation: 80 characters.
- Code lines: no hard limit, but prefer readability over compactness.

---

## 15. Extension Modules

Each `ext/*` directory is an independent Go module:

- Has its own `go.mod` and `go.sum`.
- Must compile and lint cleanly with `GOWORK=off`.
- Must reference a released version of the core relay module (no `replace` directive in CI).
- Coverage threshold: 75%.
- Tag format: `ext/<name>/v0.X.Y` — push tag after merging the PR.

---

## 16. Makefile Targets

```bash
make setup   # Install dev tools (lefthook, golangci-lint)
make fmt     # Run gofmt -s -w .
make lint    # Run golangci-lint run ./...
make test    # Run go test -v -cover ./...
make tidy    # Run go mod tidy for core and all ext modules
make clean   # Remove build artefacts and test cache
make all     # fmt + lint + tidy + test (default)
```

---

*Last updated: 2026-04-04*
