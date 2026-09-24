#!/bin/sh

# Exercises the POSIX installer against a locally-built, checksummed release.
# Nothing is downloaded from GitHub, and the installed binary never leaves the
# runner's temporary directory.

set -eu

repo_root=$(CDPATH= cd -P -- "$(dirname -- "$0")/../.." && pwd)
smoke_root=$(mktemp -d "${TMPDIR:-/tmp}/mhl-installer-smoke.XXXXXX")
smoke_tag=v0.0.0-ci
smoke_version=${smoke_tag#v}
smoke_server_pid=""

cleanup() {
  if [ -n "$smoke_server_pid" ]; then
    kill "$smoke_server_pid" 2>/dev/null || true
    wait "$smoke_server_pid" 2>/dev/null || true
  fi
  rm -rf "$smoke_root"
}
trap cleanup EXIT HUP INT TERM

case "$(uname -s)" in
  Darwin) smoke_os=darwin ;;
  Linux) smoke_os=linux ;;
  *) printf 'unsupported smoke-test OS\n' >&2; exit 1 ;;
esac

case "$(uname -m)" in
  arm64|aarch64) smoke_arch=arm64 ;;
  x86_64|amd64) smoke_arch=amd64 ;;
  *) printf 'unsupported smoke-test architecture\n' >&2; exit 1 ;;
esac

if [ "$smoke_os" = darwin ] && [ "$smoke_arch" != arm64 ]; then
  printf 'the published macOS target is darwin-arm64, got %s\n' "$smoke_arch" >&2
  exit 1
fi

asset_root="$smoke_root/releases"
release_dir="$asset_root/$smoke_tag"
stage_dir="$smoke_root/stage"
install_dir="$smoke_root/install/bin"
archive="mhl-${smoke_version}-${smoke_os}-${smoke_arch}.tar.gz"
mkdir -p "$release_dir" "$stage_dir" "$install_dir"

sh -n "$repo_root/install.sh"
sh -n "$repo_root/uninstall.sh"

(
  cd "$repo_root/src/mhl-runtime"
  CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/mh-language/mhl-core-runtime/internal/cli.Version=${smoke_version}" \
    -o "$stage_dir/mhl" ./cmd/mhl
)
tar -C "$stage_dir" -czf "$release_dir/$archive" mhl

if command -v sha256sum >/dev/null 2>&1; then
  archive_hash=$(sha256sum "$release_dir/$archive" | awk '{print $1}')
else
  archive_hash=$(shasum -a 256 "$release_dir/$archive" | awk '{print $1}')
fi
printf '%s  %s\n' "$archive_hash" "$archive" > "$release_dir/checksums.txt"

python3 -m http.server 8765 --bind 127.0.0.1 --directory "$asset_root" \
  >"$smoke_root/http.log" 2>&1 &
smoke_server_pid=$!

server_ready=false
attempt=0
while [ "$attempt" -lt 30 ]; do
  if curl -fsS "http://127.0.0.1:8765/$smoke_tag/checksums.txt" >/dev/null 2>&1; then
    server_ready=true
    break
  fi
  attempt=$((attempt + 1))
  sleep 0.2
done
[ "$server_ready" = true ] || {
  cat "$smoke_root/http.log" >&2
  printf 'local artifact server did not start\n' >&2
  exit 1
}

PATH="$install_dir:$PATH" \
MHL_VERSION="$smoke_tag" \
MHL_BASE_URL="http://127.0.0.1:8765" \
MHL_INSTALL_DIR="$install_dir" \
  sh "$repo_root/install.sh"

installed_version=$("$install_dir/mhl" version)
case "$installed_version" in
  *"$smoke_version"*) ;;
  *) printf 'installed binary reported unexpected version: %s\n' "$installed_version" >&2; exit 1 ;;
esac

MHL_INSTALL_DIR="$install_dir" sh "$repo_root/uninstall.sh" --keep-vscode
[ ! -e "$install_dir/mhl" ] || {
  printf 'uninstaller left the runtime behind\n' >&2
  exit 1
}

printf 'installer smoke passed for %s-%s\n' "$smoke_os" "$smoke_arch"
