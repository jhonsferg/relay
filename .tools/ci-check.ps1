# ci-check.ps1 — Replicates CI pipeline checks locally before creating a PR.
# Run from the repo root:  ./ci-check.ps1
# Requires: go, golangci-lint (v2), benchstat (optional)
# Exit code: 0 = all checks pass

$ErrorActionPreference = "Stop"
$Global:LASTEXITCODE = $null
$root = Get-Location
$passed = 0
$failed = 0
$skipped = 0

function Step($name, $script) {
    Write-Host "`n━━━ $name ━━━" -ForegroundColor Cyan
    try {
        & $script
        if ($Global:LASTEXITCODE -and $Global:LASTEXITCODE -ne 0) { throw "exit code $Global:LASTEXITCODE" }
        Write-Host "  ✓ $name" -ForegroundColor Green
        $script:passed++
    } catch {
        Write-Host "  ✗ $name : $_" -ForegroundColor Red
        $script:failed++
    }
}

function Skip($name, $reason) {
    Write-Host "  ~ $name (skipped: $reason)" -ForegroundColor Yellow
    $script:skipped++
}

# ── Prerequisites ──────────────────────────────────────────────────────────
Step "go version" { go version }
Step "golangci-lint version" { golangci-lint --version }

# ── 1. Core module tests ──────────────────────────────────────────────────
Step "go test (core, no race)" {
    go test -count=1 -coverprofile=".\coverage.out" -covermode=atomic ./...
    Remove-Item ".\coverage.out" -ErrorAction SilentlyContinue | Out-Null
}

# ── 2. go vet (core) ──────────────────────────────────────────────────────
Step "go vet (core)" { go vet ./... }

# ── 3. golangci-lint (core) ───────────────────────────────────────────────
Step "golangci-lint (core)" { golangci-lint run --timeout=5m ./... }

# ── 4. WASM build check ──────────────────────────────────────────────────
Step "WASM build" {
    $env:GOOS = "js"; $env:GOARCH = "wasm"
    go build ./...
    Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
}

# ── 5. go mod verify ─────────────────────────────────────────────────────
Step "go mod verify" { go mod verify }

# ── 6. Build examples ────────────────────────────────────────────────────
Step "go build examples" {
    Push-Location examples
    try { $env:GOWORK="off"; go build ./... } finally { Pop-Location; Remove-Item Env:GOWORK -ErrorAction SilentlyContinue }
}

# ── 7. Extension modules ─────────────────────────────────────────────────
$extMods = Get-ChildItem ext -Recurse -Filter go.mod | ForEach-Object { $_.DirectoryName }

foreach ($dir in $extMods) {
    $name = "go test ($dir)"
    Step $name {
        Push-Location $dir
        try { go test -count=1 ./... } finally { Pop-Location }
    }

    $name2 = "go vet ($dir)"
    Step $name2 {
        Push-Location $dir
        try { go vet ./... } finally { Pop-Location }
    }
}

# ── 8. golangci-lint (extensions) — non-blocking (CI uses || true) ─────
foreach ($dir in $extMods) {
    $name = "golangci-lint ($dir)"
    Write-Host "  ? $name (non-blocking)" -ForegroundColor Yellow
    Push-Location $dir
    try {
        $out = golangci-lint run --timeout=3m ./... 2>&1
        if ($LASTEXITCODE -ne 0) {
            Write-Host "    pre-existing issues (CI ignores)" -ForegroundColor DarkYellow
            $out | ForEach-Object { Write-Host "    $_" -ForegroundColor DarkYellow }
        } else {
            Write-Host "    V OK" -ForegroundColor Green
        }
    } finally { Pop-Location }
}

# ── 9. Benchstat regression check (optional, manual) ─────────────────────
Skip "benchstat" "run manually when needed (workflow_dispatch)"

# ── 10. Nancy supply chain scan (optional, needs OSS Index creds) ─────────
Skip "nancy" "needs OSS Index credentials (401 without them)"

# ── Summary ───────────────────────────────────────────────────────────────
Write-Host "`n═══════════════════════════════════════" -ForegroundColor Cyan
Write-Host "RESULTS:  ✓ $passed  ✗ $failed  ~ $skipped" -ForegroundColor $(if ($failed -eq 0) { "Green" } else { "Red" })

if ($failed -gt 0) {
    exit 1
}
