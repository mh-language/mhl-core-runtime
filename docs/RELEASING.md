# Releasing

This repository ships three products. Each has its own version, tag and `release.sh`:

| Product | Tag | Script | Version lives in | Published by | Published to |
|---|---|---|---|---|---|
| Language (`mhl` CLI) | `vX.Y.Z` | `src/mhl-runtime/release.sh` | the tag only (`mhl version` reads `git describe`) | `.github/workflows/release.yml` | GitHub Release (binaries, archives, checksums, a universal VSIX for `install.sh`) |
| VS Code extension | `extensions-vX.Y.Z` | `src/vscode-mhl/release.sh` | `package.json` and `package-lock.json` (`"mhlRuntime"` pins the bundled language release) | `.github/workflows/publish-extension.yml` | VS Marketplace and Open VSX |
| mhl studio | `studio-vX.Y.Z` | `src/mhl-studio/release.sh` | `tauri.conf.json`, `package.json`, `package-lock.json`, `Cargo.toml` | `.github/workflows/studio-release.yml` | GitHub Release, never marked "Latest" (unsigned installers) |

No script publishes anything from your machine. Each one does four things:

1. Runs the checks CI will run.
2. Refuses to go on unless the tree is clean and in sync with `origin/main`.
3. Refuses to go on unless `CHANGELOG.md` has the version's section.
4. Pushes the tag. GitHub Actions does the rest.

Every script takes `--dry-run`, which runs the checks and stops before tagging. Every script takes `-h` for its usage.

## Order, when releasing more than one

Release them in this order:

1. **Language first.** The extension bundles a *released* language version: its script refuses an `mhlRuntime` that is not a tag on `origin`. The studio's sidecar takes its version from `git describe` over the `v*` tags, so it reports the new version only once that tag exists.
2. **Extension next**, pinned to the new language tag (`--bump X.Y.Z --runtime vA.B.C`).
3. **Studio last.**

Each product can also be released alone. The language never waits for the studio: the studio's checks are not part of the language's release gate.

## Choosing the version

Read the product's `## Unreleased` section:

- **Fixed only** → patch (`1.14.0` → `1.14.1`).
- **Added**, or a **Changed** that keeps existing programs working → minor (`1.14.0` → `1.15.0`).
- A change that breaks an existing program or a published contract → think twice.
  - Language: major. Or minor, if the "Changed" entry tells users how to keep the old behaviour, as the stage-inputs change did.
  - Studio and extension (0.x): minor.
- **Prerelease**: `v1.15.0-beta.1`. Only the language and the studio take one. The extension's version must be plain `X.Y.Z`.

To list what changed since the last release:

```sh
git tag --sort=-creatordate | head            # latest tags of all three
git log --oneline v1.14.0..HEAD               # since a language tag
git log --oneline studio-v0.7.0..HEAD -- src/mhl-studio
git log --oneline extensions-v0.10.0..HEAD -- src/vscode-mhl
```

## Before any release

- [ ] On `main`, clean tree, `git pull`. Every change is committed **and pushed**: the scripts compare `HEAD` with `origin/main`.
- [ ] `## Unreleased` in the product's `CHANGELOG.md` lists everything user-visible. Its section becomes the release notes.
- [ ] Docs reflect the change. The language reference is `docs/site/`, deployed on push by `page.yml`. Skills pass: `MHL=src/mhl-runtime/dist/mhl skills/check.sh`.

## 1. Language — `vX.Y.Z`

```sh
cd src/mhl-runtime
./release.sh --bump v1.15.0
#   CHANGELOG.md (repo root): "## Unreleased" → "## 1.15.0 — <today>",
#   with a fresh, empty "## Unreleased" above it. Nothing else changes.
git diff ../../CHANGELOG.md               # review
git commit -am "chore(release): 1.15.0"
git push
./release.sh v1.15.0 --dry-run            # optional: the checks only
./release.sh v1.15.0
```

`./release.sh v1.15.0` refuses unless every one of these holds:

- you are on `main`;
- the tree is clean;
- `CHANGELOG.md` has `## 1.15.0`;
- local `main` equals `origin/main`;
- the tag doesn't exist yet.

Then it runs CI's checks in order: `go vet`, `make build`, `make test` and `make functional-test`. Last, it runs `make verify-release`, a cross-compile dry run that you can skip with `--no-verify-release`. When all pass, it pushes the tag.

**Watch:** https://github.com/mh-language/mhl-core-runtime/actions/workflows/release.yml

**Verify:**

```sh
gh release view v1.15.0                   # assets: archives per OS, checksums, VSIX
curl -fsSL https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/install.sh | MHL_VERSION=1.15.0 sh
mhl version                               # reports v1.15.0
```

## 2. VS Code extension — `extensions-vX.Y.Z`

```sh
cd src/vscode-mhl
./release.sh --bump 0.11.0 --runtime v1.15.0
#   package.json + package-lock.json → 0.11.0, "mhlRuntime" → v1.15.0,
#   CHANGELOG.md: "## Unreleased" → "## 0.11.0" (no date, no new "## Unreleased")
git diff                                   # review
git commit -am "chore(release): extension 0.11.0"
git push
./release.sh 0.11.0 --dry-run              # optional
./release.sh 0.11.0
```

- Leave out `--runtime` to keep the bundled language version. The bump then prints the latest local `v*` tag, in case you meant to pin it.
- The script checks the following, and only then pushes the tag:
  - package.json, package-lock.json and CHANGELOG.md agree;
  - the tree is clean and in sync with `origin/main`;
  - `mhlRuntime` is a tag on `origin` (so release the language first).

  Before pushing, it packages the universal VSIX and the one for your platform with the workflow's pinned `vsce`. That needs `go`, which builds the bundled runtime.
- The workflow builds one VSIX per platform plus a universal one. It publishes to the Marketplace through Trusted Publishing (OIDC: no token). It publishes to Open VSX with the `OPEN_VSX` secret. If you rename `publish-extension.yml`, update the Marketplace's Trusted Publishing policy, which names that file.
- **After release**, add `## Unreleased` back at the top of `src/vscode-mhl/CHANGELOG.md`. The bump doesn't recreate it.

**Watch:** https://github.com/mh-language/mhl-core-runtime/actions/workflows/publish-extension.yml

**Verify:** the Marketplace and Open VSX pages show the new version. In VS Code, update the extension and open a `.mh` file: diagnostics and completion come from the bundled `bin/mhl`, the pinned language version.

## 3. mhl studio — `studio-vX.Y.Z`

```sh
cd src/mhl-studio
./release.sh --bump 0.8.0
#   tauri.conf.json, package.json, package-lock.json, Cargo.toml → 0.8.0.
#   It does NOT touch CHANGELOG.md:
```

Edit `src/mhl-studio/CHANGELOG.md` by hand. Rename `## Unreleased` to `## 0.8.0` and add a fresh, empty `## Unreleased` above it. Then:

```sh
git diff                                   # review: four version files + CHANGELOG
git commit -am "chore(release): studio 0.8.0"
git push
./release.sh 0.8.0 --dry-run               # optional
./release.sh 0.8.0
```

- The script checks that the four files agree, that `## 0.8.0` exists, that the tree is clean and in sync, and that the tag is new.
- Then it runs `npm ci`, `npm run sidecar`, `npm run typecheck` and `npm test`. If those change a tracked file, it stops: commit the result first.
- CI also runs `cargo clippy`. Run it locally to avoid a red build: `cd src-tauri && cargo clippy --all-targets -- -D warnings && cargo test`.
- The workflow reruns studio.yml's checks, then builds the macOS, Windows and Linux installers on their own runners. That takes about 15–20 minutes. Each installer embeds the language runtime built for its target.
- It creates a **draft** release and publishes it only when every installer is uploaded. It never marks the release "Latest", which stays the language's. The installers are unsigned for now.

**Watch:** https://github.com/mh-language/mhl-core-runtime/actions/workflows/studio-release.yml

**Verify:** `gh release view studio-v0.8.0`, and the download page (`docs/site/download.html`, which reads the GitHub releases API) offers it.

## When something goes wrong

| Refusal | Fix |
|---|---|
| `working tree is dirty` | Commit or stash. |
| `local main is not in sync with origin/main` | Run `git pull` or `git push`. The tag must point at what is on GitHub. |
| `CHANGELOG.md has no "## X.Y.Z" section` | Run the product's `--bump`. For the studio, also rename the CHANGELOG heading by hand. |
| `"## Unreleased" is empty; nothing to release` | Write the CHANGELOG entries first. |
| extension: `mhlRuntime … is not a tag on origin` | Release the language first, or pin an existing tag with `--bump X.Y.Z --runtime vA.B.C`. |
| studio: `… says A but tauri.conf.json says B` | Run `./release.sh --bump <version>` again. |
| `tag … already exists` | That version is taken. Bump to the next one. |

If the workflow fails **after the tag was pushed**:

- **Nothing was published yet** (the release has no assets, or the studio's is still a draft):
  1. Fix the problem on `main`.
  2. Delete the tag: `git push origin :refs/tags/<tag> && git tag -d <tag>`.
  3. Delete the draft or empty release, if one was created: `gh release delete <tag>`.
  4. Run the script again.
- **Something was published** (a Marketplace version, release assets people may have downloaded):
  - Don't reuse the version. Fix the problem and release the next patch.
  - The Marketplace never accepts the same extension version twice.
