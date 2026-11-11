# Changelog

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
