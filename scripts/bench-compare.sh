#!/usr/bin/env bash
# bench-compare.sh — compare benchmark results between two Git refs locally.
#
# Usage:
#   ./scripts/bench-compare.sh [baseline-ref] [pr-ref] [threshold]
#
# Arguments:
#   baseline-ref  Git ref to use as baseline (default: master)
#   pr-ref        Git ref to compare against baseline (default: HEAD)
#   threshold     Regression threshold as a percentage integer (default: 10)
#
# Examples:
#   ./scripts/bench-compare.sh
#   ./scripts/bench-compare.sh master my-feature-branch
#   ./scripts/bench-compare.sh master HEAD 5
#
# Requirements:
#   - benchstat: go install golang.org/x/perf/cmd/benchstat@latest
#   - git, go

set -euo pipefail

BASELINE_REF="${1:-master}"
PR_REF="${2:-HEAD}"
THRESHOLD="${3:-10}"

BENCH_ARGS="-bench=. -benchmem -run=^$ -count=6"
BENCH_PKGS="./benchmarks/..."

RESULTS_DIR="bench_results"

# ── Helpers ──────────────────────────────────────────────────────────────────

die() { echo "ERROR: $*" >&2; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "'$1' not found. Install it with: $2"
}

# ── Preflight checks ─────────────────────────────────────────────────────────

require_cmd benchstat "go install golang.org/x/perf/cmd/benchstat@latest"
require_cmd go        "https://go.dev/dl/"

# Resolve refs to full SHAs so we can restore the working tree afterwards
CURRENT_BRANCH=$(git symbolic-ref --short HEAD 2>/dev/null || git rev-parse HEAD)
BASELINE_SHA=$(git rev-parse --verify "$BASELINE_REF") \
  || die "Cannot resolve baseline ref: $BASELINE_REF"
PR_SHA=$(git rev-parse --verify "$PR_REF") \
  || die "Cannot resolve PR ref: $PR_REF"

if [ -n "$(git status --porcelain)" ]; then
  die "Working tree is dirty. Stash or commit your changes first."
fi

mkdir -p "$RESULTS_DIR"
BASELINE_FILE="$RESULTS_DIR/baseline.txt"
PR_FILE="$RESULTS_DIR/pr.txt"

echo "═══════════════════════════════════════════════════════"
echo "  Relay benchmark regression comparison"
echo "  Baseline : $BASELINE_REF ($BASELINE_SHA)"
echo "  PR       : $PR_REF ($PR_SHA)"
echo "  Threshold: >${THRESHOLD}% regression fails"
echo "  Packages : $BENCH_PKGS"
echo "  Count    : 6 (per benchstat recommendations)"
echo "═══════════════════════════════════════════════════════"
echo ""

cleanup() {
  echo ""
  echo "Restoring original branch/commit: $CURRENT_BRANCH"
  git checkout --quiet "$CURRENT_BRANCH" || true
}
trap cleanup EXIT

# ── Run benchmarks on baseline ───────────────────────────────────────────────

echo "→ Checking out baseline: $BASELINE_REF"
git checkout --quiet "$BASELINE_SHA"

echo "→ Running benchmarks on baseline (this may take a while)…"
# shellcheck disable=SC2086
go test $BENCH_ARGS $BENCH_PKGS 2>&1 | tee "$BASELINE_FILE"
echo ""

# ── Run benchmarks on PR branch ──────────────────────────────────────────────

echo "→ Checking out PR ref: $PR_REF"
git checkout --quiet "$PR_SHA"

echo "→ Running benchmarks on PR branch (this may take a while)…"
# shellcheck disable=SC2086
go test $BENCH_ARGS $BENCH_PKGS 2>&1 | tee "$PR_FILE"
echo ""

# ── Compare with benchstat ───────────────────────────────────────────────────

echo "═══════════════════════════════════════════════════════"
echo "  benchstat comparison: baseline vs PR"
echo "═══════════════════════════════════════════════════════"
echo ""
BENCHSTAT_OUTPUT=$(benchstat "$BASELINE_FILE" "$PR_FILE" 2>&1)
echo "$BENCHSTAT_OUTPUT"
echo ""

# ── Detect regressions ───────────────────────────────────────────────────────

REGRESSIONS=$(echo "$BENCHSTAT_OUTPUT" | awk -v threshold="$THRESHOLD" '
  /^goos|^goarch|^pkg|^cpu|^$|^name/ { next }
  {
    for (i = 1; i <= NF; i++) {
      if ($i ~ /^\+[0-9]+\.[0-9]+%$/) {
        val = $i
        gsub(/[+%]/, "", val)
        if (val + 0 > threshold + 0) {
          print "  REGRESSION:", $0
        }
      }
    }
  }
')

if [ -n "$REGRESSIONS" ]; then
  echo "⚠️  Regressions detected (>${THRESHOLD}%):"
  echo "$REGRESSIONS"
  echo ""
  echo "Results saved to:"
  echo "  Baseline : $BASELINE_FILE"
  echo "  PR branch: $PR_FILE"
  exit 1
else
  echo "✅ No regressions detected (threshold: >${THRESHOLD}%)"
  echo ""
  echo "Results saved to:"
  echo "  Baseline : $BASELINE_FILE"
  echo "  PR branch: $PR_FILE"
fi
