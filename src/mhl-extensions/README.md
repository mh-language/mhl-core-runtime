# Official MHL extensions

This directory owns the source, tests, and distribution of the official packages:

| Package | Kind | Backend |
| --- | --- | --- |
| [mhl-blob-s3](mhl-blob-s3/) | blob | S3-compatible object storage |
| [mhl-cache-redis](mhl-cache-redis/) | cache | Redis with TTL |
| [mhl-sql-postgres](mhl-sql-postgres/) | sql | PostgreSQL data queries |
| [mhl-store-postgres](mhl-store-postgres/) | store | PostgreSQL |
| [mhl-store-redis](mhl-store-redis/) | store | Redis durable runtime state |
| [mhl-store-sqlite](mhl-store-sqlite/) | store | Local SQLite |

Each package keeps its own Go module and dependencies. The `EXTENSIONS` variable
in [Makefile](Makefile) is the authoritative distribution list.

## Release policy

Runtime/VS Code releases use `vX.Y.Z`. Extension bundles use
`extensions-vX.Y.Z`, with optional SemVer prerelease/build suffixes, and can ship
independently. A bundle contains all six packages; its version does **not**
replace each package's version. When a package changes, update both its
`extension.mh` version and `extVersion` in `main.go`. CI rejects disagreement.

Each release contains one `<package>.tar.gz` per package, `SHA256SUMS`, and
`release.json`. The JSON records package versions, API versions, hashes, target
platforms, Go toolchain, and the exact source/runtime commit tested. Compatibility
is established with the runtime from that commit; the release does not imply
compatibility with every older runtime speaking the same API version.

The archives include Linux, macOS, and Windows binaries for amd64 and arm64,
with CGO disabled. macOS Intel remains available for existing consumers even
though current runtime releases publish macOS ARM64 only. CI cross-compiles all
six targets and exercises the Linux amd64 binaries against real services.
Cross-compilation alone does not establish native execution coverage elsewhere.

Releases are published with `--latest=false`, so `install.sh` and `install.ps1`
continue to discover the runtime through GitHub's latest-release endpoint.
Prerelease tags also set GitHub's prerelease flag. Existing releases are never
overwritten by the workflow; corrections require a new tag.

## Build and test locally

Run from the repository root with Go (the version in the module files), Python 3,
and Docker Compose for service integration:

```sh
make -C src/mhl-runtime build
make -C src/mhl-extensions vet test release
python3 -m unittest discover -s src/mhl-extensions/scripts -p 'test_*.py'
python3 src/mhl-extensions/scripts/package_release.py prepare \
  --runtime "$PWD/src/mhl-runtime/dist/mhl"
make -C src/mhl-extensions verify-release
```

`make release` creates archives and their checksums. `prepare` additionally
records bundle metadata and regenerates checksums to include `release.json`.
Without `--tag`, the bundle is a local `snapshot`. Build outputs are ignored by
Git and are not release inputs; CI always compiles the selected commit.

For integration, start each service using its package's `make up` target, then:

```sh
make -C src/mhl-extensions smoke-release MHL="$PWD/src/mhl-runtime/dist/mhl"
```

This serves the archives on an ephemeral loopback HTTP port and runs each
package's smoke script with an archive URL plus SHA-256. It checks installation,
host binary selection, `extension doctor`, and operations through the runtime.
SQLite needs no service; run it alone with:

```sh
python3 src/mhl-extensions/scripts/package_release.py smoke \
  --runtime "$PWD/src/mhl-runtime/dist/mhl" --extension mhl-store-sqlite
```

Use each service package's `make down` afterward to remove its test containers
and volumes. CI performs this cleanup even after failures.

## Publish a bundle

After merging the changes and version updates, tag the intended commit:

```sh
git tag -a extensions-v0.1.0 -m "Official extensions 0.1.0"
git push origin extensions-v0.1.0
```

[Release official extensions](../../.github/workflows/release-extensions.yml)
resolves the existing tag to a commit and calls the same
[Extensions CI](../../.github/workflows/extensions.yml) used for PRs. It runs
runtime/package tests and vet, cross-compiles, validates archive contents and
complete checksums, and runs service integration using the packaged binaries.
Only validated artifacts from that workflow run reach the publishing job.

Manual dispatch accepts an **existing** extension tag, for example to retry a
failed build before publication. It cannot publish a branch or replace a release.
If publication failed leaving a GitHub draft, inspect that draft before deciding
how to recover; the workflow deliberately refuses to overwrite it.

The runtime Makefile ignores extension tags when deriving `mhl version`. The
runtime release workflow also hides those refs only in its disposable checkout
before GoReleaser discovers the current/previous runtime tags.

## Install

Choose an extension bundle from the
[repository releases](https://github.com/mh-language/mhl-core-runtime/releases),
copy the package hash from that release's `SHA256SUMS`, and run, replacing
`<bundle-tag>` and `<sha256>`:

```sh
mhl extension install 'https://github.com/mh-language/mhl-core-runtime/releases/download/<bundle-tag>/mhl-store-postgres.tar.gz#sha256=<sha256>'
mhl extension doctor
```

The runtime selects and vendors only the host binary. Installing a source-only
Git subdirectory does not build it; use release assets for prebuilt installation.

Previous releases in the former `mh-language/mhl-extensions` repository remain
the source for their existing URLs and pinned hashes. Do not move/delete them
or replace their assets. New bundles are published here; existing installed
packages and lock files do not need to be rewritten solely for this migration.
