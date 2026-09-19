#!/bin/sh
# Uninstalls the mhl runtime and the VS Code extension installed by install.sh.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/uninstall.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/uninstall.sh | sh -s -- --purge
#
# Env overrides:
#   MHL_INSTALL_DIR  where the binary was installed (default: $HOME/.mhl/bin)

# --purge also removes user-wide MHL data under $HOME/.mhl, including extensions.
# Project-local .mhl directories are never removed.

set -eu

: "${HOME:?HOME must be set}"
INSTALL_DIR="${MHL_INSTALL_DIR:-$HOME/.mhl/bin}"
MHL_HOME="$HOME/.mhl"
purge=false
keep_vscode=false

info() { printf 'mhl-uninstall: %s\n' "$1"; }
die() { printf 'mhl-uninstall: error: %s\n' "$1" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage: uninstall.sh [--purge] [--keep-vscode]

  --purge        also remove ~/.mhl, including user-wide extensions
  --keep-vscode  do not uninstall the MHL VS Code extension
  -h, --help     show this help
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --purge) purge=true ;;
    --keep-vscode) keep_vscode=true ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1" ;;
  esac
  shift
done

binary="$INSTALL_DIR/mhl"
if [ -e "$binary" ] || [ -L "$binary" ]; then
  rm -f "$binary"
  info "removed $binary"
else
  info "runtime not found at $binary; nothing to remove"
fi

# install.sh adds this exact two-line block. Remove only that block so a PATH
# entry maintained by the user or another package manager is left untouched.
marker="# added by mhl install.sh"
path_line="export PATH=\"$INSTALL_DIR:\$PATH\""
for rc_file in "$HOME/.zshrc" "$HOME/.bashrc" "$HOME/.profile"; do
  [ -f "$rc_file" ] || continue
  rc_tmp="$(mktemp "${TMPDIR:-/tmp}/mhl-uninstall.XXXXXX")"
  awk -v marker="$marker" -v path_line="$path_line" '
    $0 == marker { pending = $0; next }
    pending != "" {
      if ($0 == path_line) { pending = ""; next }
      print pending
      pending = ""
    }
    { print }
    END { if (pending != "") print pending }
  ' "$rc_file" > "$rc_tmp"
  if ! cmp -s "$rc_file" "$rc_tmp"; then
    cat "$rc_tmp" > "$rc_file"
    info "removed the installer-managed PATH entry from $rc_file"
  fi
  rm -f "$rc_tmp"
done

if [ "$keep_vscode" = false ]; then
  if command -v code >/dev/null 2>&1; then
    if code --uninstall-extension mhl-language.mhl-language >/dev/null 2>&1; then
      info "uninstalled the MHL VS Code extension"
    else
      info "MHL VS Code extension was not installed (or VS Code could not remove it)"
    fi
  else
    info "VS Code CLI ('code') not found; skipping extension removal"
  fi
fi

if [ "$purge" = true ]; then
  case "$MHL_HOME" in
    ""|/|"$HOME") die "refusing to purge unsafe path: $MHL_HOME" ;;
  esac
  if [ -e "$MHL_HOME" ]; then
    rm -rf "$MHL_HOME"
    info "purged user-wide MHL data from $MHL_HOME"
  fi
else
  # Remove directories only when they became empty; preserve extensions and
  # any other user data below ~/.mhl.
  rmdir "$INSTALL_DIR" 2>/dev/null || true
  rmdir "$MHL_HOME" 2>/dev/null || true
fi

info "done. Open a new shell for the PATH change to take effect."
