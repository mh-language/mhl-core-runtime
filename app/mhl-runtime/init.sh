#!/usr/bin/env bash
# Idempotent setup/build for the mhl-runtime Go project.
# Safe to run repeatedly; returns non-zero if setup or build fails.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

if ! command -v go >/dev/null 2>&1; then
  echo "FAIL: Go toolchain not found on PATH" >&2
  exit 1
fi

echo "==> go version: $(go version)"

if [ ! -f go.mod ]; then
  echo "FAIL: go.mod missing in $SCRIPT_DIR" >&2
  exit 1
fi

echo "==> Downloading modules"
go mod download

echo "==> Tidying modules"
go mod tidy

echo "==> Building all packages"
go build ./...

echo "OK: mhl-runtime setup and build complete"
