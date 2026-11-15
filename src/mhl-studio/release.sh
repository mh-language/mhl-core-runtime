#!/usr/bin/env bash
# Cuts an mhl studio release: checks what .github/workflows/studio-release.yml
# will check, runs the studio's fast checks, then pushes a studio-vX.Y.Z tag.
# The push triggers that workflow, which builds the macOS, Windows and Linux
# installers on their own runners and publishes them to a GitHub Release.
# Nothing is built for release or published from this machine.
#
# The version lives in four files that must agree — bump them, add a "## X.Y.Z"
# section to CHANGELOG.md (it becomes the release notes), and commit first:
#   ./release.sh --bump 0.2.0   # writes tauri.conf.json, package.json,
#                               # package-lock.json and Cargo.toml (no commit)
#
# Usage:
#   ./release.sh              # releases tauri.conf.json's version
#   ./release.sh 0.2.0        # same, but fails unless the files say 0.2.0
#   ./release.sh --dry-run    # run every check, don't tag or push
#
# The studio is versioned on its own, independent of the runtime's vX.Y.Z tags
# and the extension's extensions-vX.Y.Z tags.

set -euo pipefail

REMOTE="origin"
MAIN_BRANCH="main"
WORKFLOW_URL="https://github.com/mh-language/mhl-core-runtime/actions/workflows/studio-release.yml"

info() { printf 'release: %s\n' "$1"; }
die() { printf 'release: error: %s\n' "$1" >&2; exit 1; }
need_cmd() { command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"; }
is_version() { printf '%s' "$1" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$'; }

expected=""
bump=""
dry_run=0
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) dry_run=1 ;;
    --bump)
      shift
      [ $# -gt 0 ] || die "--bump needs a version, e.g. --bump 0.2.0"
      bump="$1"
      ;;
    -h|--help)
      sed -n '2,19p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    v*|studio-v*) die "pass the bare version (e.g. 0.2.0), not a tag: $1" ;;
    [0-9]*.*.*) expected="$1" ;;
    *) die "unexpected argument: $1" ;;
  esac
  shift
done

need_cmd git
need_cmd node
need_cmd npm

cd "$(dirname "${BASH_SOURCE[0]}")"
[ -f src-tauri/tauri.conf.json ] || die "src-tauri/tauri.conf.json not found next to release.sh"

# --- bump: write the version everywhere, then stop -----------------------------
if [ -n "$bump" ]; then
  is_version "$bump" || die "version must look like 1.2.3 or 1.2.3-beta.1 (got: $bump)"
  # Replace only the top-level "version" value, keeping the file's formatting.
  node -e '
    const fs = require("fs");
    const f = "src-tauri/tauri.conf.json";
    const src = fs.readFileSync(f, "utf8");
    const out = src.replace(/^(  "version":\s*")[^"]*(")/m, `$1${process.argv[1]}$2`);
    if (out === src || JSON.parse(out).version !== process.argv[1]) throw new Error(`could not set the version in ${f}`);
    fs.writeFileSync(f, out);
  ' "$bump"
  npm version "$bump" --no-git-tag-version --allow-same-version >/dev/null
  # Only the [package] version: the first `version = ` line of Cargo.toml.
  awk -v v="$bump" '!done && /^version = "/ { print "version = \"" v "\""; done = 1; next } { print }' \
    src-tauri/Cargo.toml > src-tauri/Cargo.toml.tmp && mv src-tauri/Cargo.toml.tmp src-tauri/Cargo.toml
  info "set $bump in tauri.conf.json, package.json, package-lock.json and Cargo.toml"
  grep -Eq "^## \[?$(printf '%s' "$bump" | sed 's/\./\\./g')\]?([[:space:]]|$)" CHANGELOG.md \
    || info "add a \"## $bump\" section to CHANGELOG.md (it becomes the release notes)"
  info "review, commit and push, then run ./release.sh $bump"
  exit 0
fi

version="$(node -p "require('./src-tauri/tauri.conf.json').version")"
pkg_version="$(node -p "require('./package.json').version")"
lock_version="$(node -p "require('./package-lock.json').version")"
cargo_version="$(sed -n 's/^version = "\(.*\)"$/\1/p' src-tauri/Cargo.toml | head -n1)"
tag="studio-v$version"

is_version "$version" || die "tauri.conf.json version must look like 1.2.3 (got: $version)"
if [ -n "$expected" ] && [ "$expected" != "$version" ]; then
  die "tauri.conf.json says $version, not $expected — run: ./release.sh --bump $expected"
fi
for pair in "package.json:$pkg_version" "package-lock.json:$lock_version" "Cargo.toml:$cargo_version"; do
  [ "${pair#*:}" = "$version" ] \
    || die "${pair%%:*} says ${pair#*:} but tauri.conf.json says $version — run: ./release.sh --bump $version"
done
# The release workflow publishes this section as the release notes.
grep -Eq "^## \[?$(printf '%s' "$version" | sed 's/\./\\./g')\]?([[:space:]]|$)" CHANGELOG.md \
  || die "CHANGELOG.md has no \"## $version\" section"
[ -f THIRD_PARTY_NOTICES.md ] || die "THIRD_PARTY_NOTICES.md is missing — run: npm run notices"

# --- preflight: the tag must point at what is on origin/main ------------------
branch="$(git rev-parse --abbrev-ref HEAD)"
[ "$branch" = "$MAIN_BRANCH" ] || die "on branch '$branch', expected '$MAIN_BRANCH'"

[ -z "$(git status --porcelain)" ] || die "working tree is dirty; commit or stash first"

info "fetching $REMOTE"
git fetch --quiet "$REMOTE" "$MAIN_BRANCH" --tags

[ "$(git rev-parse HEAD)" = "$(git rev-parse "$REMOTE/$MAIN_BRANCH")" ] \
  || die "local $MAIN_BRANCH is not in sync with $REMOTE/$MAIN_BRANCH (pull/push first)"

if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  die "tag $tag already exists locally — bump the version (./release.sh --bump X.Y.Z)"
fi
if git ls-remote --exit-code --tags "$REMOTE" "refs/tags/$tag" >/dev/null 2>&1; then
  die "tag $tag already exists on $REMOTE — bump the version (./release.sh --bump X.Y.Z)"
fi

# --- the studio's fast checks (studio.yml runs them again, plus clippy) --------
need_cmd go
info "npm ci"
npm ci --no-audit --no-fund
info "sidecar, typecheck, tests"
npm run sidecar
npm run typecheck
npm test

[ -z "$(git status --porcelain)" ] \
  || die "the checks changed tracked files (see git status); commit the result first"

# --- tag & push ---------------------------------------------------------------
if [ "$dry_run" -eq 1 ]; then
  info "dry run: $tag would be released; not tagging or pushing"
  exit 0
fi

info "tagging $tag"
git tag -a "$tag" -m "mhl studio $version"

info "pushing $tag to $REMOTE"
git push "$REMOTE" "$tag"

info "done. watch the build (about 15–20 min), then the release goes public on its own:"
info "  $WORKFLOW_URL"
