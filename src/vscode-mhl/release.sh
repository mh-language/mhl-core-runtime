#!/usr/bin/env bash
# Cuts a VS Code extension release: checks what .github/workflows/publish-extension.yml
# will check, packages the extension the same way it does, then pushes an
# extensions-vX.Y.Z tag. The push triggers that workflow, which publishes one VSIX
# per platform target (each bundling the runtime release pinned in package.json's
# "mhlRuntime") plus a universal one, to the Visual Studio Marketplace (Trusted
# Publishing, no stored token) and to Open VSX (the OPEN_VSX repository secret).
# Nothing is published from this machine.
#
# The version comes from package.json, so bump it and commit first:
#   ./release.sh --bump 0.7.1 [--runtime v1.2.0]
#       # package.json + package-lock.json say 0.7.1, CHANGELOG.md's "## Unreleased"
#       # becomes "## 0.7.1", and --runtime pins "mhlRuntime" (no commit, no tag)
#   # review, commit, push main
#
# Usage:
#   ./release.sh              # releases package.json's version
#   ./release.sh 0.7.1        # same, but fails unless package.json says 0.7.1
#   ./release.sh --dry-run    # run every check and package, don't tag or push
#
# The extension is versioned on its own, independent of the runtime's vX.Y.Z tags;
# "mhlRuntime" names the released runtime it bundles. Needs go to build it.

set -euo pipefail

REMOTE="origin"
MAIN_BRANCH="main"
VSCE="@vscode/vsce@4.1.0" # keep in sync with publish-extension.yml
WORKFLOW_URL="https://github.com/mh-language/mhl-core-runtime/actions/workflows/publish-extension.yml"

info() { printf 'release: %s\n' "$1"; }
die() { printf 'release: error: %s\n' "$1" >&2; exit 1; }
need_cmd() { command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"; }

is_version() { printf '%s' "$1" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; }
is_runtime_tag() { printf '%s' "$1" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; }
# has_section VERSION: CHANGELOG.md has a "## VERSION" (or "## [VERSION]") heading.
has_section() { grep -Eq "^## \[?$(printf '%s' "$1" | sed 's/\./\\./g')\]?([[:space:]]|$)" CHANGELOG.md; }

expected=""
bump=""
runtime=""
dry_run=0
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) dry_run=1 ;;
    --bump)
      shift
      [ $# -gt 0 ] || die "--bump needs a version, e.g. --bump 0.7.1"
      bump="$1"
      ;;
    --runtime)
      shift
      [ $# -gt 0 ] || die "--runtime needs a runtime tag, e.g. --runtime v1.2.0"
      runtime="$1"
      ;;
    -h|--help)
      sed -n '2,23p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    v*|extensions-v*) die "pass the bare version (e.g. 0.7.1), not a tag: $1" ;;
    [0-9]*.*.*) expected="$1" ;;
    *) die "unexpected argument: $1" ;;
  esac
  shift
done

need_cmd git
need_cmd node
need_cmd npm

cd "$(dirname "${BASH_SOURCE[0]}")"
[ -f package.json ] || die "package.json not found next to release.sh"

# --- bump: write the version (and optionally mhlRuntime), then stop ------------
if [ -n "$bump" ]; then
  [ -z "$expected" ] || die "--bump takes the version itself; don't also pass $expected"
  case "$bump" in extensions-v*|v*) die "pass the bare version (e.g. 0.7.1), not a tag: $bump" ;; esac
  is_version "$bump" || die "version must look like 1.2.3 (got: $bump)"
  [ -z "$runtime" ] || is_runtime_tag "$runtime" \
    || die "--runtime must be a runtime release tag like v1.2.0 (got: $runtime)"
  has_section "$bump" && die "CHANGELOG.md already has a \"## $bump\" section"
  grep -q '^## Unreleased[[:space:]]*$' CHANGELOG.md || die "CHANGELOG.md has no \"## Unreleased\" section"
  # Only non-blank lines between "## Unreleased" and the next "## " count.
  awk '/^## Unreleased[[:space:]]*$/ { f = 1; next } /^## / { f = 0 } f && NF { n++ } END { exit n == 0 }' \
    CHANGELOG.md || die "\"## Unreleased\" is empty; nothing to release"

  npm version "$bump" --no-git-tag-version --allow-same-version >/dev/null
  if [ -n "$runtime" ]; then
    # Replace only the "mhlRuntime" value, keeping the file's formatting.
    node -e '
      const fs = require("fs");
      const src = fs.readFileSync("package.json", "utf8");
      const out = src.replace(/^(  "mhlRuntime":\s*")[^"]*(")/m, `$1${process.argv[1]}$2`);
      if (JSON.parse(out).mhlRuntime !== process.argv[1]) throw new Error("could not set mhlRuntime in package.json");
      fs.writeFileSync("package.json", out);
    ' "$runtime"
  fi
  # The extension's sections carry no date (the Marketplace shows this file).
  awk -v heading="## $bump" '!done && /^## Unreleased[[:space:]]*$/ { print heading; done = 1; next } { print }' \
    CHANGELOG.md > CHANGELOG.md.tmp && mv CHANGELOG.md.tmp CHANGELOG.md

  info "set $bump in package.json and package-lock.json; CHANGELOG.md's \"## Unreleased\" is now \"## $bump\""
  pinned="$(node -p "require('./package.json').mhlRuntime || ''")"
  if [ -n "$runtime" ]; then
    info "mhlRuntime is now $pinned"
  else
    latest="$(git tag -l 'v[0-9]*' --sort=-v:refname | head -n1)"
    info "mhlRuntime stays $pinned${latest:+ (latest local runtime tag: $latest; pass --runtime $latest to bundle it)}"
  fi
  info "review, commit and push, then run ./release.sh $bump"
  exit 0
fi
[ -z "$runtime" ] || die "--runtime only goes with --bump"

need_cmd go

version="$(node -p "require('./package.json').version")"
lock_version="$(node -p "require('./package-lock.json').version")"
runtime_tag="$(node -p "require('./package.json').mhlRuntime || ''")"
tag="extensions-v$version"

is_version "$version" \
  || die "package.json version must look like 1.2.3 (got: $version)"
if [ -n "$expected" ] && [ "$expected" != "$version" ]; then
  die "package.json says $version, not $expected — run: ./release.sh --bump $expected"
fi
[ "$lock_version" = "$version" ] \
  || die "package-lock.json says $lock_version but package.json says $version — run: npm install"
has_section "$version" \
  || die "CHANGELOG.md has no \"## $version\" section — run: ./release.sh --bump $version"

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

# The workflow builds the bundled runtime from this tag, so it must be released.
[ -n "$runtime_tag" ] || die "package.json has no \"mhlRuntime\" (the runtime release tag to bundle)"
git ls-remote --exit-code --tags "$REMOTE" "refs/tags/$runtime_tag" >/dev/null 2>&1 \
  || die "mhlRuntime $runtime_tag is not a tag on $REMOTE — release the runtime first, or pin a released one"

# --- package exactly as the workflow does -------------------------------------
# The VSIXs go to a temp dir and are thrown away: they only prove the workflow's
# package jobs will succeed (the universal one, and this host's target as a
# stand-in for the rest). The workflow builds the ones it publishes.
out_dir="$(mktemp -d)"
trap 'rm -rf "$out_dir"' EXIT

info "npm ci"
npm ci --no-audit --no-fund

info "npm test"
npm test

info "packaging the universal VSIX with $VSCE"
rm -rf bin
npx --yes "$VSCE" package --out "$out_dir/mhl-language-universal.vsix"

host_target="$(node scripts/vendor-runtime.mjs --host-target)"
info "vendoring mhl $runtime_tag for $host_target"
node scripts/vendor-runtime.mjs --target "$host_target"
info "packaging the $host_target VSIX with $VSCE"
npx --yes "$VSCE" package --target "$host_target" --out "$out_dir/mhl-language-$host_target.vsix"

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
