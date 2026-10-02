#!/usr/bin/env bash
# Publishes the VS Code extension to the Open VSX registry (https://open-vsx.org),
# the marketplace used by VSCodium, Cursor, Windsurf, Gitpod, and other forks.
#
# It first runs ./install.sh (npm install + `vsce package` + local install), then
# uploads the resulting mhl-language-<version>.vsix with `ovsx publish`.
#
# The Open VSX access token is read from OPEN_VSX in the workspace-root .env
# (gitignored); it is never printed. Get one at
# https://open-vsx.org → Settings → Access Tokens.
#
# Usage:
#   ./release.sh
#   ./release.sh --dry-run   # build/package only, don't publish

set -eu

cd "$(dirname "${BASH_SOURCE[0]}")"

info() { printf 'release: %s\n' "$1"; }
die() { printf 'release: error: %s\n' "$1" >&2; exit 1; }

dry_run=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) dry_run=1 ;;
    -h|--help)
      sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) die "unexpected argument: $arg" ;;
  esac
done

# --- load OPEN_VSX from the workspace-root .env -------------------------------
ENV_FILE="$(cd .. && pwd)/.env"
[ -f "$ENV_FILE" ] || die "missing $ENV_FILE (expected an OPEN_VSX=... line)"
set -a
. "$ENV_FILE"
set +a
[ -n "${OPEN_VSX:-}" ] || die "OPEN_VSX is not set in $ENV_FILE"

# --- build & package ------------------------------------------------------------
info "running ./install.sh"
./install.sh

VERSION=$(node -p "require('./package.json').version")
NAMESPACE=$(node -p "require('./package.json').publisher")
VSIX_FILE="mhl-language-${VERSION}.vsix"
[ -f "$VSIX_FILE" ] || die "$VSIX_FILE not found after install.sh"

# --- publish ------------------------------------------------------------------
if [ "$dry_run" -eq 1 ]; then
  info "dry run: built $VSIX_FILE; not publishing"
  exit 0
fi

# Ensure the namespace exists. This is a one-time setup, but re-running it is
# harmless: ovsx exits non-zero with "Namespace already exists" once it's there,
# which we treat as success (older ovsx has no idempotent flag).
info "ensuring Open VSX namespace '$NAMESPACE' exists"
if create_out=$(npx --yes ovsx create-namespace "$NAMESPACE" -p "$OPEN_VSX" 2>&1); then
  info "namespace '$NAMESPACE' created"
else
  case "$create_out" in
    *"already exists"*) info "namespace '$NAMESPACE' already exists — ok" ;;
    *) die "create-namespace failed: $create_out" ;;
  esac
fi

info "publishing $VSIX_FILE to Open VSX"
npx --yes ovsx publish "$VSIX_FILE" -p "$OPEN_VSX"

info "done: https://open-vsx.org/extension/${NAMESPACE}/mhl-language"
