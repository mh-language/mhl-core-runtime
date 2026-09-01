#!/bin/sh
# Installs the mhl runtime and, if VS Code is present, the mhl-language
# extension. Usage:
#   curl -fsSL https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/install.sh | sh
#
# Env overrides:
#   MHL_VERSION   release tag to install (default: latest)
#   MHL_BASE_URL  base URL releases are downloaded from
#                 (default: https://github.com/mh-language/mhl-core-runtime/releases/download)
#   MHL_INSTALL_DIR  where the binary is placed (default: $HOME/.mhl/bin)

set -eu

REPO="mh-language/mhl-core-runtime"
BASE_URL="${MHL_BASE_URL:-https://github.com/${REPO}/releases/download}"
INSTALL_DIR="${MHL_INSTALL_DIR:-$HOME/.mhl/bin}"

info() { printf 'mhl-install: %s\n' "$1"; }
die() { printf 'mhl-install: error: %s\n' "$1" >&2; exit 1; }

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

need_cmd curl
need_cmd tar

os="$(uname -s)"
arch="$(uname -m)"

case "$os" in
  Darwin) mhl_os="darwin" ;;
  Linux) mhl_os="linux" ;;
  *) die "unsupported OS: $os (only linux and darwin have published binaries; see the Releases page)" ;;
esac

case "$arch" in
  arm64|aarch64) mhl_arch="arm64" ;;
  x86_64|amd64) mhl_arch="amd64" ;;
  *) die "unsupported architecture: $arch" ;;
esac

if [ "$mhl_os" = "darwin" ] && [ "$mhl_arch" != "arm64" ]; then
  die "darwin-amd64 (Intel Mac) has no published release; only darwin-arm64 is supported on macOS"
fi

tag="${MHL_VERSION:-}"
if [ -z "$tag" ]; then
  info "resolving latest release..."
  tag="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | head -n1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
  [ -n "$tag" ] || die "could not resolve latest release version"
fi
# A manual MHL_VERSION may be given with or without the leading "v" the repo's
# tags use (e.g. "1.2.0-beta.1" or "v1.2.0-beta.1") — normalize to the tag.
case "$tag" in
  v*) ;;
  *) tag="v${tag}" ;;
esac
# GitHub's release-download URL is keyed by the git tag (with "v"), but
# GoReleaser's archive/checksum filenames use {{.Version}} — the tag with
# that leading "v" stripped. Conflating the two 404s: mhl-v1.2.0-... isn't a
# real asset name, only mhl-1.2.0-... is.
version="${tag#v}"

archive="mhl-${version}-${mhl_os}-${mhl_arch}.tar.gz"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

info "downloading ${archive} (${tag})..."
curl -fsSL -o "$work_dir/$archive" "$BASE_URL/$tag/$archive"
curl -fsSL -o "$work_dir/checksums.txt" "$BASE_URL/$tag/checksums.txt"

info "verifying checksum..."
expected="$(grep " ${archive}\$" "$work_dir/checksums.txt" | awk '{print $1}')"
[ -n "$expected" ] || die "no checksum entry found for ${archive}"

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$work_dir/$archive" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$work_dir/$archive" | awk '{print $1}')"
else
  die "need sha256sum or shasum to verify the download"
fi
[ "$expected" = "$actual" ] || die "checksum mismatch for ${archive} (expected $expected, got $actual)"

mkdir -p "$INSTALL_DIR"
tar -C "$work_dir" -xzf "$work_dir/$archive"
mv "$work_dir/mhl" "$INSTALL_DIR/mhl"
chmod +x "$INSTALL_DIR/mhl"
info "installed mhl to ${INSTALL_DIR}/mhl"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    rc_file=""
    case "${SHELL:-}" in
      */zsh) rc_file="$HOME/.zshrc" ;;
      */bash) rc_file="$HOME/.bashrc" ;;
      *) rc_file="$HOME/.profile" ;;
    esac
    line="export PATH=\"$INSTALL_DIR:\$PATH\""
    if [ -f "$rc_file" ] && grep -qF "$line" "$rc_file" 2>/dev/null; then
      : # already present
    else
      printf '\n# added by mhl install.sh\n%s\n' "$line" >> "$rc_file"
      info "added ${INSTALL_DIR} to PATH in ${rc_file} (restart your shell, or run: source ${rc_file})"
    fi
    ;;
esac

vsix="$(grep -o 'mhl-language-[^"[:space:]]*\.vsix' "$work_dir/checksums.txt" | head -n1 || true)"
if [ -n "$vsix" ]; then
  info "downloading VS Code extension (${vsix})..."
  curl -fsSL -o "$work_dir/$vsix" "$BASE_URL/$tag/$vsix"

  vsix_expected="$(grep " ${vsix}\$" "$work_dir/checksums.txt" | awk '{print $1}')"
  if command -v sha256sum >/dev/null 2>&1; then
    vsix_actual="$(sha256sum "$work_dir/$vsix" | awk '{print $1}')"
  else
    vsix_actual="$(shasum -a 256 "$work_dir/$vsix" | awk '{print $1}')"
  fi
  if [ "$vsix_expected" != "$vsix_actual" ]; then
    info "warning: checksum mismatch for ${vsix}, skipping extension install"
  elif command -v code >/dev/null 2>&1; then
    code --install-extension "$work_dir/$vsix" --force
    info "installed the mhl VS Code extension"
  else
    dest="$HOME/Downloads/$vsix"
    mkdir -p "$HOME/Downloads"
    cp "$work_dir/$vsix" "$dest"
    info "VS Code CLI ('code') not found on PATH; saved the extension to ${dest}"
    info "install it manually via VS Code: Extensions -> ... -> Install from VSIX..."
  fi
else
  info "no VS Code extension found in this release; skipping"
fi

info "done. verify with: ${INSTALL_DIR}/mhl (prints usage)"
