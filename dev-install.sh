#!/bin/sh
# Builds mhl — and, unless --runtime-only, the vscode-mhl extension — from
# this local checkout and installs them exactly where install.sh's release
# installer would, so a change made here can be tried immediately in any
# other repo without cutting a release first. Usage:
#   ./dev-install.sh [--runtime-only]
#
# Env overrides:
#   MHL_INSTALL_DIR  where the binary is placed (default: $HOME/.mhl/bin) —
#                     must match what's already on your PATH (install.sh
#                     uses the same default), or the "mhl" you run elsewhere
#                     won't pick this build up.

set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RUNTIME_DIR="$SCRIPT_DIR/src/mhl-runtime"
EXT_DIR="$SCRIPT_DIR/vscode-mhl"
INSTALL_DIR="${MHL_INSTALL_DIR:-$HOME/.mhl/bin}"

info() { printf 'dev-install: %s\n' "$1"; }
die() { printf 'dev-install: error: %s\n' "$1" >&2; exit 1; }

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

runtime_only=false
for arg in "$@"; do
  case "$arg" in
    --runtime-only) runtime_only=true ;;
    -h|--help)
      cat <<'EOF'
Usage: dev-install.sh [--runtime-only]

Builds mhl from this checkout's src/mhl-runtime and installs it to
$MHL_INSTALL_DIR (default: ~/.mhl/bin) — the same place install.sh's release
installer uses, so both the "mhl" on your PATH and vscode-mhl's default
mhl.serverPath pick it up. Also rebuilds and reinstalls the vscode-mhl
extension itself (skipped automatically if npm or the VS Code CLI aren't
found, or always with --runtime-only) — needed when a change touches the
extension's own code, not just the runtime (e.g. syntax highlighting).

  --runtime-only  skip rebuilding/reinstalling the VS Code extension
  -h, --help      show this help
EOF
      exit 0
      ;;
    *) die "unknown option: $arg" ;;
  esac
done

need_cmd go
need_cmd make

info "building mhl from ${RUNTIME_DIR#"$SCRIPT_DIR"/} ..."
make -C "$RUNTIME_DIR" build >/dev/null

mkdir -p "$INSTALL_DIR"
# mv (a same-filesystem rename) rather than cp: atomic, so a concurrently
# running `mhl lsp`/`mhl run` keeps serving its already-open old binary
# instead of hitting a "text file busy" mid-copy error.
mv -f "$RUNTIME_DIR/dist/mhl" "$INSTALL_DIR/mhl"
chmod +x "$INSTALL_DIR/mhl"
info "installed $("$INSTALL_DIR/mhl" --version) to $INSTALL_DIR/mhl"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) info "warning: $INSTALL_DIR is not on your PATH right now — 'mhl' elsewhere won't see this build until it is" ;;
esac

if [ "$runtime_only" = true ]; then
  info "done (--runtime-only: left the VS Code extension as installed)"
  exit 0
fi

if ! command -v npm >/dev/null 2>&1; then
  info "npm not found; skipping the VS Code extension (pass --runtime-only to silence this, or install Node to pick it up too)"
  exit 0
fi
if ! command -v code >/dev/null 2>&1; then
  info "VS Code CLI ('code') not found; skipping the VS Code extension (pass --runtime-only to silence this)"
  exit 0
fi

info "building vscode-mhl from ${EXT_DIR#"$SCRIPT_DIR"/} ..."
(cd "$EXT_DIR" && npm install --no-audit --no-fund >/dev/null && npm run compile >/dev/null)

info "packaging the extension..."
rm -f "$EXT_DIR"/*.vsix
(cd "$EXT_DIR" && npx --yes @vscode/vsce package >/dev/null)

vsix="$(find "$EXT_DIR" -maxdepth 1 -name '*.vsix' -print -quit)"
[ -n "$vsix" ] || die "vsce package did not produce a .vsix"

# Some VS Code CLI builds still use Node's deprecated url.parse() API and
# emit DEP0169 — suppress deprecations for this child process only, same as
# install.sh.
NODE_OPTIONS="${NODE_OPTIONS:+$NODE_OPTIONS }--no-deprecation" \
  code --install-extension "$vsix" --force >/dev/null
info "installed the mhl VS Code extension from $(basename "$vsix")"

info "done. Reload any open VS Code window (Developer: Reload Window) to pick up the new extension/LSP."
