#!/usr/bin/env bash
# Cuts a VS Code extension release: checks what .github/workflows/publish-extension.yml
# will check, packages the extension the same way it does, then pushes an
# extensions-vX.Y.Z tag. The push triggers that workflow, which publishes the one
# VSIX to the Visual Studio Marketplace (Trusted Publishing, no stored token) and to
# Open VSX (the OPEN_VSX repository secret). Nothing is published from this machine.
#
# The version comes from package.json, so bump it and commit first:
#   npm version 0.7.1 --no-git-tag-version   # updates package.json + package-lock.json
#   # add a "## 0.7.1" section to CHANGELOG.md, commit, push main
#
# Usage:
#   ./release.sh              # releases package.json's version
#   ./release.sh 0.7.1        # same, but fails unless package.json says 0.7.1
#   ./release.sh --dry-run    # run every check and package, don't tag or push
#
# The extension is versioned on its own, independent of the runtime's vX.Y.Z tags.

set -euo pipefail

REMOTE="origin"
MAIN_BRANCH="main"
VSCE="@vscode/vsce@4.1.0" # keep in sync with publish-extension.yml
WORKFLOW_URL="https://github.com/mh-language/mhl-core-runtime/actions/workflows/publish-extension.yml"

info() { printf 'release: %s\n' "$1"; }
die() { printf 'release: error: %s\n' "$1" >&2; exit 1; }
need_cmd() { command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"; }

expected=""
dry_run=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) dry_run=1 ;;
    -h|--help)
      sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    v*|extensions-v*) die "pass the bare version (e.g. 0.7.1), not a tag: $arg" ;;
    [0-9]*.*.*) expected="$arg" ;;
    *) die "unexpected argument: $arg" ;;
  esac
done

need_cmd git
need_cmd node
need_cmd npm

cd "$(dirname "${BASH_SOURCE[0]}")"
[ -f package.json ] || die "package.json not found next to release.sh"

version="$(node -p "require('./package.json').version")"
lock_version="$(node -p "require('./package-lock.json').version")"
tag="extensions-v$version"

printf '%s' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' \
  || die "package.json version must look like 1.2.3 (got: $version)"
if [ -n "$expected" ] && [ "$expected" != "$version" ]; then
  die "package.json says $version, not $expected — run: npm version $expected --no-git-tag-version"
fi
[ "$lock_version" = "$version" ] \
  || die "package-lock.json says $lock_version but package.json says $version — run: npm install"
grep -Eq "^## \[?$(printf '%s' "$version" | sed 's/\./\\./g')\]?([[:space:]]|$)" CHANGELOG.md \
  || die "CHANGELOG.md has no \"## $version\" section"

# --- preflight: the tag must point at what is on origin/main ------------------
branch="$(git rev-parse --abbrev-ref HEAD)"
[ "$branch" = "$MAIN_BRANCH" ] || die "on branch '$branch', expected '$MAIN_BRANCH'"

[ -z "$(git status --porcelain)" ] || die "working tree is dirty; commit or stash first"

info "fetching $REMOTE"
git fetch --quiet "$REMOTE" "$MAIN_BRANCH" --tags

[ "$(git rev-parse HEAD)" = "$(git rev-parse "$REMOTE/$MAIN_BRANCH")" ] \
  || die "local $MAIN_BRANCH is not in sync with $REMOTE/$MAIN_BRANCH (pull/push first)"

if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  die "tag $tag already exists locally — bump the version in package.json"
fi
if git ls-remote --exit-code --tags "$REMOTE" "refs/tags/$tag" >/dev/null 2>&1; then
  die "tag $tag already exists on $REMOTE — bump the version in package.json"
fi

# --- package exactly as the workflow does -------------------------------------
# The VSIX goes to a temp dir and is thrown away: it only proves the workflow's
# package job will succeed. The workflow builds the one it publishes.
out_dir="$(mktemp -d)"
trap 'rm -rf "$out_dir"' EXIT

info "npm ci"
npm ci --no-audit --no-fund

info "packaging with $VSCE"
npx --yes "$VSCE" package --out "$out_dir/mhl-language.vsix"

[ -z "$(git status --porcelain)" ] \
  || die "packaging changed tracked files (see git status); commit the result first"

# --- tag & push ---------------------------------------------------------------
if [ "$dry_run" -eq 1 ]; then
  info "dry run: $tag would be released; not tagging or pushing"
  exit 0
fi

info "tagging $tag"
git tag -a "$tag" -m "VS Code extension $version"

info "pushing $tag to $REMOTE"
git push "$REMOTE" "$tag"

info "done. watch the publish run:"
info "  $WORKFLOW_URL"
