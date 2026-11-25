<p align="center">
  <img src="docs/site/assets/mhl-logo.png" alt="mhl logo" width="160">
</p>

# Meta-Harness Language (mhl)

**mhl** is a declarative language for describing AI agent pipelines: agents, tools, memory, MCP servers, prompts, and the pipelines that wire them together. **[Run your first MHL workflow](https://mh-language.github.io/mhl-core-runtime/start.html)**.

## Install

The install scripts download the `mhl` binary and, if VS Code is installed, the `mhl-language`
extension, both from the latest [GitHub Release](https://github.com/mh-language/mhl-core-runtime/releases).
The [download page](https://mh-language.github.io/mhl-core-runtime/download.html) picks the right
file for your system: mhl studio, the CLI and the VS Code extension.

**macOS / Linux:**

```bash
curl -fsSL https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/install.ps1 | iex
```

This installs `mhl` to `~/.mhl/bin` (`%LOCALAPPDATA%\mhl\bin` on Windows) and adds it to your
PATH. Supported platforms today: `linux-amd64`, `linux-arm64`, `darwin-arm64` (Apple Silicon),
`windows-amd64`, `windows-arm64`. Intel Mac (`darwin-amd64`) has no published binary.

### Install a specific version (alpha / beta)

By default the scripts install the newest **stable** release and skip prereleases. To install a
specific tag — an alpha or beta included, such as `1.5.0-alpha.5` — set `MHL_VERSION` (with or
without the leading `v`):

```bash
curl -fsSL https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/install.sh | MHL_VERSION=1.10.1 sh
```

```powershell
$env:MHL_VERSION = "1.10.1"
irm https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/install.ps1 | iex
```

On macOS/Linux the variable must be set on the `sh` side of the pipe, as above. Run the installer
again without `MHL_VERSION` to go back to the latest stable release. `MHL_INSTALL_DIR` changes
where the binary is placed.

### Uninstall

The uninstall scripts remove the runtime, the installer-managed PATH entry, and the MHL VS Code
extension. They preserve user-wide extensions and project-local `.mhl` state by default.

**macOS / Linux:**

```bash
curl -fsSL https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/uninstall.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/uninstall.ps1 | iex
```

For a full user-wide cleanup on macOS/Linux, run:

```bash
curl -fsSL https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/uninstall.sh | sh -s -- --purge
```

On Windows, download the PowerShell script and run `./uninstall.ps1 -Purge`. Purge removes
`~/.mhl` (and `%LOCALAPPDATA%\mhl` on Windows), including user-wide extensions; it never removes
project-local `.mhl` directories. Pass `--keep-vscode` / `-KeepVSCode` to keep the editor extension.

### Manual install

Download a binary and/or the `.vsix` directly from the
[Releases page](https://github.com/mh-language/mhl-core-runtime/releases), or build from source:

```bash
# runtime
(cd src/mhl-runtime && make build)   # outputs dist/mhl
# without make:
(cd src/mhl-runtime && ./build.sh build)

# VS Code extension
cd src/vscode-mhl && npm install && npx @vscode/vsce package   # outputs mhl-language-<version>.vsix
```

Then install the `.vsix` in VS Code via **Extensions → ⋯ → Install from VSIX...**, or run
`src/vscode-mhl/install.sh`, which builds and installs it in one step.

## Documentation

The v1.4 language profile is in release-candidate stabilization.

Browse **[guides by task](https://mh-language.github.io/mhl-core-runtime/docs.html)** or open the **[full language reference](https://mh-language.github.io/mhl-core-runtime/Docs-Reference.dc.html)**.


## Examples

`sample/` has worked, self-verifying `.mh` examples for every language
feature — run any of them directly with `mhl test <file>`:

- [`sample/syntax/`](sample/syntax/README.md) — the expression and statement language itself
  (arithmetic, arrays, objects, strings, conditionals, loops)
- [`sample/features/`](sample/features/README.md) — the higher-level declarations a pipeline
  is built from (`agent`, `memory`, `prompt`) and how a `pipeline` wires them together

## Repository layout

- [`src/mhl-runtime/`](src/mhl-runtime) — the Go implementation of the `mhl` CLI (parser,
  interpreter, runtime, LSP)
- [`src/vscode-mhl/`](src/vscode-mhl) — the VS Code extension (syntax highlighting, diagnostics,
  completion), a thin wrapper around `mhl lsp`
- [`src/mhl-studio/`](src/mhl-studio) — mhl studio, the optional desktop IDE: a workflow diagram
  kept in sync with the `.mh` code, with live runs
- [`docs/site/`](docs/site) — the canonical language reference, deployed to GitHub Pages
- [`skills/`](skills) — agent skills that teach models to write `.mh`
- [`sample/`](sample) — worked `.mh` examples, doubling as the docs-facing test suite
- [`tests_e2e/cloud/`](tests_e2e/cloud) — scenarios that aren't `go test`, exercising
  `mhl serve mcp` across a pod fleet

## Official extensions

The official external extensions used by MHL (`mhl-blob-s3`, `mhl-cache-redis`,
`mhl-sql-postgres`, `mhl-store-postgres`, `mhl-store-redis`, `mhl-store-sqlite`) live in
their own repository, [`mh-language/mhl-packages`](https://github.com/mh-language/mhl-packages) —
split out so their Docker-backed integration CI and six-platform release builds don't slow
down runtime iteration here. See that repo for source, build/release instructions, and
`mhl extension install` usage. Releases published before the split remain under this
repository's `extensions-vX.Y.Z` tags.
