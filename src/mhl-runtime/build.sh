#!/bin/sh

# Makefile-compatible build entry point for environments without make.
# Run it from any directory with: ./build.sh [target]

set -eu

ROOT=$(CDPATH= cd -P -- "$(dirname -- "$0")" && pwd)
MODULE=github.com/mh-language/mhl-core-runtime
CMD=./cmd/mhl
DIST=dist
SAMPLE=../../sample

cd "$ROOT"

VERSION=$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || printf '%s\n' dev)
LDFLAGS="-s -w -X ${MODULE}/internal/cli.Version=${VERSION}"

build_host() {
    mkdir -p "$DIST" "$SAMPLE"
    CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$DIST/mhl" "$CMD"
    CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$SAMPLE/mhl" "$CMD"
}

build_target() {
    target_os=$1
    target_arch=$2
    output=$3
    mkdir -p "$(dirname -- "$output")"
    CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
        go build -trimpath -ldflags "$LDFLAGS" -o "$output" "$CMD"
}

build_release() {
    build_target linux amd64 "$DIST/linux-amd64/mhl"
    build_target darwin arm64 "$DIST/darwin-arm64/mhl"
    build_target windows amd64 "$DIST/windows-amd64/mhl.exe"
}

functional_test() {
    build_host
    "$SAMPLE/mhl" test "$SAMPLE/syntax"
    "$SAMPLE/mhl" test "$SAMPLE/features"
}

verify_release() {
    build_release
    test -s "$DIST/linux-amd64/mhl"
    test -s "$DIST/darwin-arm64/mhl"
    test -s "$DIST/windows-amd64/mhl.exe"
    echo "release verified: CGO disabled for all targets"
}

usage() {
    cat <<'EOF'
Usage: ./build.sh [target]

Targets:
  build             Build the host binary and sample/mhl (default)
  test              Run all Go tests
  functional-test   Build and run the sample test suites
  release           Build the Linux, macOS, and Windows release binaries
  linux-arm64       Build the Linux arm64 host binary
  verify-release    Build release binaries and verify they are non-empty
  clean             Remove generated release binaries
  help              Show this help
EOF
}

target=${1:-build}
case "$target" in
    build)
        build_host
        ;;
    test)
        go test ./...
        ;;
    functional-test)
        functional_test
        ;;
    release)
        build_release
        ;;
    linux-arm64)
        build_target linux arm64 "$DIST/linux-arm64/mhl"
        ;;
    verify-release)
        verify_release
        ;;
    clean)
        rm -rf "$DIST"
        ;;
    help|-h|--help)
        usage
        ;;
    *)
        echo "unknown target: $target" >&2
        usage >&2
        exit 2
        ;;
esac
