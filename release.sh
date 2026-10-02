#!/bin/sh
# Cuts a release: validates the working tree the same way CI does, then pushes a
# vX.Y.Z tag. The push triggers .github/workflows/release.yml, which cross-compiles
# the binaries, packages the archives, builds the VS Code extension, writes
# checksums, and publishes the GitHub Release with generated notes.
#
# Usage:
#   ./release.sh v0.2.0
#   ./release.sh v0.3.0-beta.1                # prerelease (GitHub marks it as such)
#   ./release.sh v0.2.0 --no-verify-release   # skip the cross-compile dry run
#   ./release.sh v0.2.0 --dry-run             # run checks, don't tag or push
#
# The tag also sets what `mhl version` reports (Makefile embeds `git describe`).

set -eu

RUNTIME_DIR="src/mhl-runtime"
REMOTE="origin"
MAIN_BRANCH="main"

info() { printf 'release: %s\n' "$1"; }
die() { printf 'release: error: %s\n' "$1" >&2; exit 1; }

need_cmd() { command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"; }

tag=""
run_verify_release=1
dry_run=0

for arg in "$@"; do
  case "$arg" in
    --no-verify-release) run_verify_release=0 ;;
    --dry-run) dry_run=1 ;;
    -h|--help)
      sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    v*.*.*) tag="$arg" ;;
    *) die "unexpected argument: $arg (expected a vX.Y.Z tag)" ;;
  esac
done

[ -n "$tag" ] || die "usage: ./release.sh vX.Y.Z[-prerelease] [--no-verify-release] [--dry-run]"
# vMAJOR.MINOR.PATCH with an optional semver prerelease (-beta.1) and/or build (+meta) suffix
printf '%s' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$' \
  || die "tag must look like v1.2.3 or v1.2.3-beta.1 (got: $tag)"

need_cmd git
need_cmd go

# --- repo root -------------------------------------------------------------
cd "$(dirname "$0")"
[ -d "$RUNTIME_DIR" ] || die "run this from the repo root ($RUNTIME_DIR not found)"

# --- preflight -----------------------------------------------------------------
branch="$(git rev-parse --abbrev-ref HEAD)"
[ "$branch" = "$MAIN_BRANCH" ] || die "on branch '$branch', expected '$MAIN_BRANCH'"

[ -z "$(git status --porcelain)" ] || die "working tree is dirty; commit or stash first"

info "fetching $REMOTE"
git fetch --quiet "$REMOTE" "$MAIN_BRANCH" --tags

local_head="$(git rev-parse HEAD)"
remote_head="$(git rev-parse "$REMOTE/$MAIN_BRANCH")"
[ "$local_head" = "$remote_head" ] \
  || die "local $MAIN_BRANCH is not in sync with $REMOTE/$MAIN_BRANCH (pull/push first)"

if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  die "tag $tag already exists locally"
fi
if git ls-remote --exit-code --tags "$REMOTE" "refs/tags/$tag" >/dev/null 2>&1; then
  die "tag $tag already exists on $REMOTE"
fi

# --- validation (mirrors .github/workflows/ci.yml, in order) ------------------
info "go vet ./..."
( cd "$RUNTIME_DIR" && go vet ./... )

info "make build"
( cd "$RUNTIME_DIR" && make build )

info "make test"
( cd "$RUNTIME_DIR" && make test )

info "make functional-test"
( cd "$RUNTIME_DIR" && make functional-test )

if [ "$run_verify_release" -eq 1 ]; then
  info "make verify-release (cross-compile dry run)"
  ( cd "$RUNTIME_DIR" && make verify-release )
fi

# --- tag & push -------------------------------------------------------------
if [ "$dry_run" -eq 1 ]; then
  info "dry run: all checks passed; not tagging or pushing"
  exit 0
fi

info "tagging $tag"
git tag -a "$tag" -m "$tag"

info "pushing $tag to $REMOTE"
git push "$REMOTE" "$tag"

info "done. watch the release build:"
info "  https://github.com/mh-language/mhl-core-runtime/actions/workflows/release.yml"
