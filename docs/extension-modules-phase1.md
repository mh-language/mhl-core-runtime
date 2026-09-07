# MHL Extension Modules v1

## Status

This document defines the Phase 1 direction for external MHL extensions. It
evolves the existing `mhl extension` workflow without introducing a second
package system or changing the runtime import grammar.

## Scope

An extension module is an external extension package containing an
`extension.mh` or `extension.json` manifest, static declarations, and one or
more host-specific executables. The package is installed before execution and
is loaded through the existing external extension protocol.

Phase 1 supports Git as the primary remote source. Archive and local-directory
installs remain supported by the current compatibility command.

## Command Surface

The singular command namespace is retained:

```text
mhl extension init <dir>
mhl extension install [--frozen]
mhl extension install <source>
mhl extension add <source>@<constraint>
mhl extension update [<id>]
mhl extension list
mhl extension doctor
mhl extension audit
```

Existing `init`, `install <source>`, `list`, and `doctor` remain compatible.
`add`, `install` without a source, and `update` are declarative operations to
be added after the integrity and lock migration work. `audit` reports lock,
materialization, integrity, source, permission, and kind-collision findings.

## Project State

The existing project lock remains canonical for runtime admission:

```text
.mhl/extensions.lock
.mhl/extensions/<extension-id>/
```

The declarative manifest will be specified before `add` is implemented. It
records direct requirements and version constraints; the lock records resolved
immutable identities. The runtime never resolves Git refs or writes project
state during `run`, `test`, `lint`, `serve`, or LSP use.

## Lockfile v2

New installs write lockfile version 2. Every remote or materialized extension
entry records its version, source, exact commit when applicable, executable
SHA-256, and a SHA-256 digest of the complete installed package tree.

The package digest is computed from sorted relative paths, normalized file
modes, byte lengths, and raw bytes. Symlinks, devices, and paths outside the
package root are not valid package content. The executable hash is retained for
platform-aware diagnostics, but the package digest is the primary integrity
anchor.

Version 1 locks remain readable for compatibility and retain their existing
executable-only verification. A mutating install migrates the lock to version
2.

## Runtime Admission

Discovery validates, before an extension can load:

1. The lock entry and manifest ID/version agree.
2. The host executable exists, is executable, and matches its pinned hash.
3. For lockfile v2, the complete materialized tree matches `package_sha256`.
4. The extension process handshake identifies the expected ID, version, and
   protocol API version.
5. Extension IDs and declared kinds do not collide in the resolved set.

The existing best-effort warning behavior remains temporarily compatible for
`mhl run`; `mhl extension doctor` and `mhl extension audit` are the explicit
strict validation commands. A later declared-install mode will fail before a
run when a direct required extension is unavailable.

## Security Boundary

Installation never executes extension code. Git resolution, downloads,
semver selection, and writes are package-manager operations only.

`permissions.secrets` remains brokered and enforced by the host. Network,
filesystem, and subprocess permissions are install-review metadata until the
runtime has OS-level sandbox enforcement; they must not be represented as an
execution sandbox today.

## Implementation Sequence

1. Add lockfile v2 and complete package-tree integrity verification.
2. Make materialization reject links and use atomic replacement.
3. Compare handshake ID/version/API with locked manifest metadata.
4. Add deterministic resolved-set validation for duplicate IDs and kinds.
5. Define the direct-requirement manifest and implement `extension add`,
   source-less `extension install`, `--frozen`, and `extension update`.
6. Add cache-by-content-digest and project materialization from cache.
7. Add `extension audit`, policy allow/deny, permission diffs, and CI
   fail-closed mode.
8. Reuse Git/cache/integrity primitives in the separate Phase 2 source-module
   implementation.

## Non-goals

- Fetching or resolving packages during program execution.
- A public registry in Phase 1.
- Treating advisory manifest permissions as sandbox enforcement.
- Replacing the current external-extension protocol.
- Changing `import` syntax. Locked MHL source modules are Phase 2.
