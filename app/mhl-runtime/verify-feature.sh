#!/usr/bin/env bash
# Idempotent per-feature verification for the mhl-runtime Go project.
# Usage: ./verify-feature.sh <feature-id>
# Runs the real build + test pipeline. Prints a concise PASS/FAIL verdict
# and exits non-zero on any failure so the harness can use the exit code.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

FEATURE_ID="${1:-all}"

if ! command -v go >/dev/null 2>&1; then
  echo "FAIL: Go toolchain not found on PATH (feature ${FEATURE_ID})" >&2
  exit 1
fi

echo "==> Verifying feature ${FEATURE_ID}"

# Build must succeed first.
if ! go build ./...; then
  echo "FAIL: build failed for feature ${FEATURE_ID}"
  exit 1
fi

# go vet as a lightweight static check.
if ! go vet ./...; then
  echo "FAIL: go vet failed for feature ${FEATURE_ID}"
  exit 1
fi

# Run the full test suite. This exercises real code via `go test`.
if ! go test ./...; then
  echo "FAIL: tests failed for feature ${FEATURE_ID}"
  exit 1
fi

echo "PASS: feature ${FEATURE_ID} verified (build + vet + tests green)"
exit 0
