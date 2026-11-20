#!/bin/sh
# Cuts a language release: validates the working tree the same way CI does, then
# pushes a vX.Y.Z tag. The push triggers .github/workflows/release.yml, which cross-compiles
# the binaries, packages the archives, builds the VS Code extension, writes
# checksums, and publishes the GitHub Release with generated notes.
#
# Usage (from src/mhl-runtime, like src/vscode-mhl/release.sh and src/mhl-studio/release.sh):
#   ./release.sh v0.2.0
#   ./release.sh v0.3.0-beta.1                # prerelease (GitHub marks it as such)
#   ./release.sh v0.2.0 --no-verify-release   # skip the cross-compile dry run
#   ./release.sh v0.2.0 --dry-run             # run checks, don't tag or push
#
# The tag also sets what `mhl version` reports (Makefile embeds `git describe`), so
# no source file holds the version. The one file a release changes is the repo-root
# CHANGELOG.md: the tag is refused unless it has a "## X.Y.Z" section. To cut one:
#   ./release.sh --bump v0.2.0   # "## Unreleased" becomes "## 0.2.0 — <today>" under
#                                # a fresh, empty "## Unreleased" (no commit, no tag)
# then review, commit and push, and run ./release.sh v0.2.0.
#
# The VS Code extension (extensions-vX.Y.Z) and mhl studio (studio-vX.Y.Z) have
# their own release.sh and tags.

set -eu

REMOTE="origin"
MAIN_BRANCH="main"

info() { printf 'release: %s\n' "$1"; }
die() { printf 'release: error: %s\n' "$1" >&2; exit 1; }

need_cmd() { command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"; }

tag=""
bump=""
run_verify_release=1
dry_run=0

while [ $# -gt 0 ]; do
  case "$1" in
    --no-verify-release) run_verify_release=0 ;;
    --dry-run) dry_run=1 ;;
    --bump)
      shift
      [ $# -gt 0 ] || die "--bump needs a version, e.g. --bump v0.2.0"
      bump="$1"
      ;;
    -h|--help)
      sed -n '2,21p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    v*.*.*) tag="$1" ;;
    *) die "unexpected argument: $1 (expected a vX.Y.Z tag)" ;;
  esac
  shift
done

# vMAJOR.MINOR.PATCH with an optional semver prerelease (-beta.1) and/or build (+meta) suffix
is_tag() { printf '%s' "$1" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'; }

# --- runtime dir: every check below runs here; git works from any subdirectory --
cd "$(dirname "$0")"
[ -f Makefile ] && [ -f go.mod ] || die "release.sh must stay in src/mhl-runtime (no Makefile/go.mod next to it)"

CHANGELOG="../../CHANGELOG.md"
[ -f "$CHANGELOG" ] || die "repo-root CHANGELOG.md not found ($CHANGELOG)"

# has_section VERSION: CHANGELOG.md has a "## VERSION" heading (optionally
# "## [VERSION]", followed by " — date" or nothing). VERSION carries no "v".
has_section() {
  pattern="$(printf '%s' "$1" | sed 's/[.+]/\\&/g')"
  grep -Eq "^## \[?${pattern}\]?([[:space:]]|$)" "$CHANGELOG"
}

# --- bump: turn "## Unreleased" into this version's section, then stop -------
if [ -n "$bump" ]; then
  [ -z "$tag" ] || die "--bump takes the version itself; don't also pass $tag"
  case "$bump" in v*) ;; *) bump="v$bump" ;; esac
  is_tag "$bump" || die "version must look like v1.2.3 or v1.2.3-beta.1 (got: $bump)"
  version="${bump#v}"
  has_section "$version" && die "CHANGELOG.md already has a \"## $version\" section"
  grep -q '^## Unreleased[[:space:]]*$' "$CHANGELOG" || die "CHANGELOG.md has no \"## Unreleased\" section"
  # Only non-blank lines between "## Unreleased" and the next "## " count.
  awk '/^## Unreleased[[:space:]]*$/ { f = 1; next } /^## / { f = 0 } f && NF { n++ } END { exit n == 0 }' \
    "$CHANGELOG" || die "\"## Unreleased\" is empty; nothing to release"
  today="$(date +%Y-%m-%d)"
  awk -v heading="## $version — $today" '
    !done && /^## Unreleased[[:space:]]*$/ { print; print ""; print heading; done = 1; next }
    { print }
  ' "$CHANGELOG" > "$CHANGELOG.tmp" && mv "$CHANGELOG.tmp" "$CHANGELOG"
  info "CHANGELOG.md: \"## Unreleased\" is now \"## $version — $today\" (a new, empty \"## Unreleased\" is above it)"
  info "review, commit and push, then run ./release.sh $bump"
  exit 0
fi

[ -n "$tag" ] || die "usage: ./release.sh vX.Y.Z[-prerelease] [--no-verify-release] [--dry-run] | --bump vX.Y.Z"
is_tag "$tag" || die "tag must look like v1.2.3 or v1.2.3-beta.1 (got: $tag)"

need_cmd git
need_cmd go

# --- preflight -----------------------------------------------------------------
branch="$(git rev-parse --abbrev-ref HEAD)"
[ "$branch" = "$MAIN_BRANCH" ] || die "on branch '$branch', expected '$MAIN_BRANCH'"

[ -z "$(git status --porcelain)" ] || die "working tree is dirty; commit or stash first"

has_section "${tag#v}" \
  || die "CHANGELOG.md has no \"## ${tag#v}\" section — run: ./release.sh --bump $tag (then commit and push)"

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
go vet ./...

info "make build"
make build

info "make test"
make test

info "make functional-test"
make functional-test

if [ "$run_verify_release" -eq 1 ]; then
  info "make verify-release (cross-compile dry run)"
  make verify-release
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
