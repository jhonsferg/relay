# RULES.md — Engineering Standards & Git Workflow
# relay — github.com/jhonsferg/relay
# Read this file at the start of every session before making any changes.

---

## 1. COMMIT RULES (ABSOLUTE)

- **NO Co-Authored-By**, no Signed-off-by, no author emails in commits — ever.
- Subject line **≤ 72 characters**, imperative mood, no trailing period.
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

### Commit scope (optional, use the package name)
Examples: `(client)`, `(retry)`, `(cache)`, `(ext/redis)`, `(ci)`, `(docs)`

### Commit body
- Add a blank line after the subject, then write the body.
- Use the body only when the *why* is non-obvious.
- Reference issues with `Closes #N` or `Refs #N` at the end of the body.
- Wrap body lines at 72 characters.

### Good examples
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

## 2. BRANCH NAMING

All branches must follow this pattern: `type/short-description`
Use lowercase, hyphens only (no underscores, no slashes beyond the prefix).

| Prefix | Purpose | Example |
|---|---|---|
| `feat/` | New feature | `feat/bulkhead-isolation` |
| `fix/` | Bug fix | `fix/race-condition-delta` |
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

## 3. GIT WORKFLOW (GITFLOW-LITE)

This project uses a simplified GitFlow adapted for a single-maintainer library.

```
master ──── always releasable, protected, tagged → auto-release
  │
  ├── feat/...      ──→ PR → squash-merge → master
  ├── fix/...       ──→ PR → squash-merge → master
  ├── hotfix/...    ──→ PR → merge (no squash) → master + tag immediately
  └── release/...   ──→ final changelog edit → PR → merge → tag
```

### Standard flow for any change
1. Create a branch from the latest `master`:
   ```bash
   git fetch origin && git checkout -b feat/my-feature origin/master
   ```
2. Make atomic commits following the commit rules above.
3. Run the local quality gate before pushing (see Section 8).
4. Push and open a PR:
   ```bash
   git push origin feat/my-feature
   gh pr create --repo jhonsferg/relay --base master \
     --title "feat(scope): short description" \
     --body "..."
   ```
5. Wait for all CI checks to pass.
6. Merge via GitHub CLI (squash for features, regular merge for hotfixes).

### Hotfix flow
```bash
git checkout -b hotfix/critical-bug origin/master
# fix, commit, push
gh pr create --repo jhonsferg/relay --base master --title "fix: ..."
# after merge, tag immediately
gh release create v0.X.Y --notes "fix: ..."
```

---

## 4. GITHUB CLI — OPERATIONS REFERENCE

### Authentication
```bash
# Verify authentication
gh auth status

# Login (if needed)
gh auth login --hostname github.com --git-protocol ssh
```

### SSH configuration for this repo
```bash
export GIT_SSH_COMMAND="ssh -i ~/.ssh/github_jhonsferg -o StrictHostKeyChecking=no"
```

### Branch protection bypass (push to master directly when needed)
```bash
# Disable enforce_admins temporarily
gh api -X DELETE repos/jhonsferg/relay/branches/master/protection/enforce_admins

# Push
git push origin master

# Re-enable immediately after
gh api -X POST repos/jhonsferg/relay/branches/master/protection/enforce_admins
```

### PR management
```bash
# Create PR
gh pr create --repo jhonsferg/relay \
  --head feat/my-feature --base master \
  --title "feat(scope): description" \
  --body "$(cat pr-body.md)"

# List open PRs
gh pr list --repo jhonsferg/relay --state open

# View PR status (checks, reviews)
gh pr view 42 --repo jhonsferg/relay

# Merge PR (squash, delete branch)
gh pr merge 42 --repo jhonsferg/relay \
  --squash --delete-branch --subject "feat(scope): description"

# Merge as admin (bypass required reviews)
gh pr merge 42 --admin --squash --delete-branch
```

### Release management
```bash
# Create a release from a tag (let goreleaser handle the body)
git tag v0.2.0 && git push origin v0.2.0
# The release.yml workflow fires automatically

# Create a lightweight release via CLI (skip goreleaser)
gh release create v0.2.0 \
  --repo jhonsferg/relay \
  --title "v0.2.0" \
  --notes "## Changes\n- feat: ..."

# List releases
gh release list --repo jhonsferg/relay

# View a release
gh release view v0.2.0 --repo jhonsferg/relay
```

### Issue management
```bash
# Create issue
gh issue create --repo jhonsferg/relay \
  --title "bug: ..." --body "..." --label "bug"

# List issues
gh issue list --repo jhonsferg/relay --state open

# Close issue
gh issue close 15 --repo jhonsferg/relay
```

### CI/CD inspection
```bash
# List recent workflow runs
gh run list --repo jhonsferg/relay --limit 10

# Watch a run in real time
gh run watch --repo jhonsferg/relay

# View failed run logs
gh run view <run-id> --repo jhonsferg/relay --log-failed

# Re-run failed jobs only
gh run rerun <run-id> --repo jhonsferg/relay --failed
```

---

## 5. VERSIONING & RELEASES

This project follows **Semantic Versioning 2.0** (semver.org).

### Version bump rules
| Change | Bump | Example |
|---|---|---|
| Breaking API change | MAJOR | `v1.0.0 → v2.0.0` |
| New feature, backward-compatible | MINOR | `v0.1.0 → v0.2.0` |
| Bug fix, backward-compatible | PATCH | `v0.1.0 → v0.1.1` |
| Security fix | PATCH (urgent) | `v0.1.0 → v0.1.1` |

### Pre-release versions
- Alpha: `v0.2.0-alpha.1`
- Beta: `v0.2.0-beta.1`
- Release candidate: `v0.2.0-rc.1`

### Release process
1. Ensure all CI checks pass on `master`.
2. Update `CHANGELOG.md` with the changes since the last tag:
   ```bash
   git log v0.1.12..HEAD --oneline --no-merges
   ```
3. Commit the changelog: `docs(changelog): update for v0.2.0`
4. Push the tag — the `release.yml` workflow triggers goreleaser automatically:
   ```bash
   git tag v0.2.0
   git push origin v0.2.0
   ```
5. Verify the release was created:
   ```bash
   gh release view v0.2.0 --repo jhonsferg/relay
   ```

### Extension module versioning
Each `ext/*` module has its own `go.mod` and version tag:
- Tag format: `ext/redis/v0.1.0`
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

## 6. LINTING — GOLANGCI-LINT V2

Config file: `.golangci.yml` (version `"2"` is mandatory).

### Running the linter
```bash
# Full project
golangci-lint run ./...

# Single file or package
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
| `goimports` ordering | `goimports -w <file>` |
| `errcheck`: unchecked error | Assign to `_` if truly ignorable, otherwise handle |
| `unused`: dead code | Remove it or add `//nolint:unused // reason` |
| `ineffassign` | Remove the useless assignment |
| `misspell` | Fix the spelling (UK locale: `colour`, `behaviour`, `licence`) |
| `gosec` false positive | `//nolint:gosec // reason` on that line only |
| `shadow` variable | Rename the inner variable |

### Inline suppression (use sparingly, always with a reason)
```go
//nolint:gosec // SHA-1 is used for cache key hashing, not security
h := sha1.New()

result, _ := doSomething() //nolint:errcheck // result always valid here
```

### golangci-lint v2 constraints (do not violate)
- Formatters (`gofmt`, `goimports`) go under `formatters:` section, NOT `linters:`.
- `gosimple` and `typecheck` do not exist in v2 — do not add them.
- `exclude-dirs` does not exist at the top level or under `issues:`.
- `misspell` locale must be `UK`.

### Pre-commit hook (lefthook)
The project uses [lefthook](https://github.com/evilmartians/lefthook). Install once:
```bash
go install github.com/evilmartians/lefthook@latest
lefthook install
```
Hooks run `gofmt` and `golangci-lint` automatically before every commit.

---

## 7. CODE DOCUMENTATION STYLE

### Godoc rules (mandatory for all exported symbols)
- Every exported type, function, method, constant and variable **must** have a doc comment.
- Comments start with the symbol name: `// FunctionName does X.`
- Use complete sentences. End with a period.
- Describe *what* and *why*, not *how* (the code shows how).

### Package-level documentation
Each package must have a `doc.go` file with a package comment:
```go
// Package cache provides an in-memory HTTP response cache implementing
// RFC 7234 cache semantics. It plugs into the relay transport stack
// via the CacheStore interface.
package cache
```

### Function and method comments
```go
// WithBulkhead returns an Option that limits the number of concurrent
// in-flight requests to maxConcurrent. If a request cannot acquire a
// slot within the request context deadline, ErrBulkheadFull is returned.
// Pass 0 to disable the bulkhead.
func WithBulkhead(maxConcurrent int) Option {
```

### Interface documentation
```go
// CacheStore is the storage backend for HTTP response caching. All
// methods must be safe for concurrent use from multiple goroutines.
type CacheStore interface {
    // Get returns the cached entry for key. Returns false if not found
    // or if the entry has expired.
    Get(key string) (*CachedResponse, bool)

    // Set stores entry under key. Implementations may evict entries
    // to respect capacity constraints.
    Set(key string, entry *CachedResponse)
}
```

### Inline comments
- Comment non-obvious logic, not self-evident code.
- Use `// NOTE:` for important observations, `// TODO:` for known gaps with an issue number.
- Use `// FIXME:` only for known bugs that need a follow-up PR — never merge with a FIXME.
- Never use `/* block comments */` in Go source (except cgo, which this project avoids).

### Example functions (testable examples)
Place examples in `example_test.go` files (or `*_example_test.go`):
```go
func ExampleClient_Execute() {
    client, _ := relay.New(relay.WithBaseURL("https://api.example.com"))
    resp, _ := client.Execute(client.Get("/users"))
    fmt.Println(resp.StatusCode)
    // Output: 200
}
```

---

## 8. CODE COVERAGE

### Thresholds (CI enforced)
| Scope | Minimum |
|---|---|
| Core library (`relay` package) | ≥ 85% |
| Extension modules (`ext/*`) | ≥ 75% |

### Measuring coverage locally
```bash
# Full coverage including non-library packages
go test -coverprofile=coverage.out -covermode=atomic ./...

# Filter to library packages only (matches CI behaviour)
grep -v -E "^github\.com/jhonsferg/relay/(cmd|examples|benchmarks)/" \
  coverage.out > coverage_lib.out

# View total
go tool cover -func=coverage_lib.out | grep '^total'

# HTML report
go tool cover -html=coverage_lib.out -o coverage.html
open coverage.html
```

### Packages excluded from coverage
These packages are intentionally excluded from the coverage calculation:
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

### Writing coverage-complete tests
- Test all exported functions including error paths.
- Test boundary conditions (nil input, empty slice, zero value).
- Use `testutil/recorder` for HTTP-level tests — avoid live network calls in unit tests.
- Use `-race` flag locally when writing concurrent code: `go test -race ./...`

---

## 9. TESTING RULES (ABSOLUTE)

- Every change must pass `go test ./...` locally before pushing.
- Always run with the race detector before opening a PR: `go test -race ./...`
- Any DATA RACE is a release blocker, not a warning.
- Table-driven tests are preferred for functions with multiple input variants.
- Test file naming: `<file>_test.go` in the same package; use `_test` suffix package
  for black-box tests that test the public API.
- Do not use `time.Sleep` in tests — use channels, `sync.WaitGroup`, or mock time.
- Known safe pattern: **never pool `httptrace.ClientTrace`** — the transport's
  `dialParallel` fires callbacks from background goroutines after `Do()` returns.
  Use `atomic.Int64` for any field written by trace callbacks (see `timing.go`).

### Test helpers and assertions
Use the project's `testutil` package for HTTP-level assertions:
```go
srv := testutil.NewMockServer(t)
srv.Enqueue(200, `{"id":1}`)
// ... use srv.URL as base URL
```

---

## 10. PERFORMANCE RULES

- Every optimisation must show a measurable improvement (> 5% in benchmarks).
- Measure with `go test -bench=. -benchmem -count=6` before AND after each change.
- Use `benchstat before.txt after.txt` to validate; attach the output in the commit body.
- Do not optimise what has not been measured. No speculative micro-optimisations.
- Profile before optimising: `go test -cpuprofile=cpu.prof -memprofile=mem.prof -bench=.`
- Analyse with: `go tool pprof cpu.prof`

---

## 11. API STABILITY RULES

- **Zero breaking changes** to exported types, functions, or interfaces without a MAJOR version bump.
- Public `Response` fields are stable: `StatusCode`, `Status`, `Headers`, `Timing`,
  `Truncated`, `RedirectCount`, `Body()`, `String()`, etc.
- New options use the functional options pattern: `WithXxx(...) Option`.
- New behaviour is always opt-in via options — never change default behaviour.
- Deprecate before removing: add `// Deprecated: use Xxx instead.` comment and keep for one minor version.
- `PutResponse(r)` is opt-in; callers who do not call it are safe (no use-after-free).

---

## 12. MEMORY SAFETY RULES

- Pooled buffers from `pool.GetSizedBuffer` must be returned via `pool.PutSizedBuffer`
  before the `Response` is handed back to the caller — the caller owns the body slice.
- `bytes.Reader` from `pool.GetBytesReader` must be released in `releasePooledReader()`,
  which is called after every `RoundTrip` attempt (success or failure).
- Never return a slice that aliases a pool buffer to the caller.
- Never store a pointer to a pooled object beyond the scope of the function that acquired it.

---

## 13. SECURITY RULES

- Run `govulncheck ./...` before any release tag.
- Never commit secrets, tokens, or credentials — use environment variables or `gh secret set`.
- TLS minimum version is `tls.VersionTLS12` (CI enforces via Trivy and CodeQL).
- `gosec` linter is enabled; suppressions require a written justification comment.
- Dependency updates: run `go get -u ./...` + `go mod tidy` monthly and open a `chore/` PR.

---

## 14. TYPOGRAPHY & STYLE RULES

- **UK English** throughout: `colour`, `behaviour`, `licence`, `initialise`, `optimise`.
- Never use the em-dash symbol (—). Use a plain hyphen ( - ) instead.
- No trailing whitespace in any file.
- Files end with a single newline character.
- Maximum line length for comments and documentation: 80 characters.
- Code lines: no hard limit, but prefer readability over compactness.

---

## 15. EXTENSION MODULES — ADDITIONAL RULES

Each `ext/*` directory is an independent Go module:
- Has its own `go.mod`, `go.sum`, `.golangci.yml` (inherits settings from root via symlink if available).
- Must compile and lint cleanly with `GOWORK=off`.
- Must reference a released version of the core relay module (not a replace directive in CI).
- Coverage ≥ 75%.
- Tag format: `ext/<name>/v0.X.Y` — push tag after merging the PR.

---

## 16. MAKEFILE TARGETS REFERENCE

```bash
make setup   # Install dev tools (lefthook, golangci-lint)
make fmt     # Run gofmt -s -w .
make lint    # Run golangci-lint run ./...
make test    # Run go test -v -cover ./...
make tidy    # Run go mod tidy for core + all ext modules
make clean   # Remove build artefacts and test cache
make all     # fmt + lint + tidy + test
```

---

## 17. WHEN TO READ ADDITIONAL CONTEXT

At the start of every session, read in this order:
1. This file (`RULES.md`) — always first
2. Session plan in `~/.copilot/session-state/*/plan.md` — current phase and open todos
3. `CHANGELOG.md` — last release and what changed
4. The specific files you are about to modify

---

*Last updated: 2026-04-04*
