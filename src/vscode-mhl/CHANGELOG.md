# Changelog

## Unreleased

## 0.13.0

### Added

- `on` in a workflow's `on step_end(step) { ... }` lifecycle hook is highlighted as a
  keyword. Its completion and diagnostics come from the bundled runtime (`mhlRuntime`)
  once it includes it.

## 0.12.0

### Added

- `concurrency` in `spawn xs concurrency 2 = ...` and `collect` in `wait xs collect` are
  highlighted as keywords. Their completion and diagnostics come from the bundled runtime
  (`mhlRuntime`) once it includes them.

## 0.11.0

### Added

- `base.` in `base.name`, the base fragment's state of a `partial` workflow, is highlighted
  as a keyword, including `base.{f} = ...` destructuring. It is contextual: a variable
  named `base` stays a name.
- `self.` and `base.` are highlighted inside a `"${...}"` interpolation too, as outside
  strings. Its completion,
  hover and go-to-definition come from the bundled runtime (`mhlRuntime`) once it includes
  `base.`.

## 0.10.0

### Added

- `.mhl` files open as mhl too — highlighting, diagnostics, completion and navigation —
  as an alias of `.mh`. Cross-file navigation into a `.mhl` sibling needs a bundled
  runtime (`mhlRuntime`) that includes the alias.

## 0.9.0

### Added

- Syntax highlighting for a fragment's stage — `partial workflow Story stage Build {`:
  `stage` as a declaration keyword and the stage's name as a label, like a `goto` target.
  Only in that form — `stage` used as a name (`input stage`, `step.stage`) stays a plain
  identifier.

## 0.8.0

### Added

- The extension bundles the `mhl` CLI (v1.11.1) on `linux-x64`, `linux-arm64`,
  `alpine-x64`, `alpine-arm64`, `darwin-x64`, `darwin-arm64`, `win32-x64` and
  `win32-arm64`, so diagnostics, completion and navigation work without installing
  it. It is also appended to `PATH` in integrated terminals. Other platforms get a
  universal package that uses `mhl` on `PATH`, as before.

### Changed

- `mhl.serverPath` defaults to empty, meaning the bundled CLI, else `mhl` on `PATH`.
  Set it to `mhl` to keep using an installed CLI instead of the bundled one.

### Fixed

- `entry` is highlighted as a keyword in a workflow's starting route
  (`entry route Choose(model: AgentModel) { ... }`), not only before `step`.

## 0.7.2

### Added

- Highlighting for the `merge` keyword, which ends a `parallel` group with an explicit
  join (`merge (branches) { ... }`), new in mhl v1.11.0.

Completion, diagnostics and running a `merge` block come from `mhl lsp` and the
runtime, so they need mhl v1.11.0 or later rather than this extension version.

### Changed

- The extension's source moved to `src/vscode-mhl/` in the repository; the
  `repository.directory` field and the README's development instructions point there.

## 0.7.1

### Added

- Highlighting for the `...` spread operator in array literals
  (`["--model", model, ...extra]`), new in mhl v1.9.0.

Signature help and completion for agent `input`s, and the diagnostics for spread
and inputs, come from `mhl lsp`, so they need mhl v1.9.0 or later rather than this
extension version.

## 0.7.0

First release on the Visual Studio Marketplace.

### Added

- Limited support for Restricted Mode: the language server keeps working, but an
  untrusted workspace can no longer choose which `mhl` binary runs.
- The server restarts automatically when a folder is trusted.
- An **Install mhl** button on the error shown when the language server can't start.

### Fixed

- **MHL: Restart Language Server** now works after a failed start, so installing
  `mhl` no longer requires reloading the window.

### Changed

- The startup error and the README now explain how to install the `mhl` CLI instead
  of how to build it from source.

## 0.6.9 and earlier

Published on the Open VSX Registry only. See the
[repository history](https://github.com/mh-language/mhl-core-runtime/commits/main/vscode-mhl).
