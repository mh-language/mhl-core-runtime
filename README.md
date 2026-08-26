<p align="center">
  <img src="docs/site/assets/mhl-logo-rebrand.png" alt="mhl logo" width="160">
</p>

# Meta-Harness Language (mhl)

**mhl** is a declarative language for describing AI agent pipelines: agents, skills, tools, memory, MCP servers, prompts, and the pipelines that wire them together. **[Click here for the full language reference](https://mh-language.github.io/mhl-core-runtime/reference.html)**.

## Install

The install scripts download the `mhl` binary and, if VS Code is installed, the `mhl-language`
extension, both from the latest [GitHub Release](https://github.com/mh-language/mhl-core-runtime/releases).

**macOS / Linux:**

```bash
curl -fsSL https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/install.ps1 | iex
```

This installs `mhl` to `~/.mhl/bin` (`%LOCALAPPDATA%\mhl\bin` on Windows) and adds it to your
PATH. Supported platforms today: `linux-amd64`, `darwin-arm64` (Apple Silicon), `windows-amd64`.

### Manual install

Download a binary and/or the `.vsix` directly from the
[Releases page](https://github.com/mh-language/mhl-core-runtime/releases), or build from source:

```bash
# runtime
cd src/mhl-runtime && make build   # outputs dist/mhl

# VS Code extension
cd vscode-mhl && npm install && npx @vscode/vsce package   # outputs mhl-language-<version>.vsix
```

Then install the `.vsix` in VS Code via **Extensions → ⋯ → Install from VSIX...**, or run
`vscode-mhl/install.sh`, which builds and installs it in one step.

## Documentation

> [!NOTE]  
> **Note:** The docs are a work in progress. The language is still evolving, and the docs
> 
The full language reference lives at **[mh-language.github.io/mhl-core-runtime/reference.html](https://mh-language.github.io/mhl-core-runtime/reference.html)**.


## Examples

[`sample/`](sample/README.md) has worked, self-verifying `.mh` examples for every language
feature — run any of them directly with `mhl test <file>`:

- [`sample/syntax/`](sample/syntax/README.md) — the expression and statement language itself
  (arithmetic, arrays, objects, strings, conditionals, loops)
- [`sample/features/`](sample/features/README.md) — the higher-level declarations a pipeline
  is built from (`agent`, `memory`, `prompt`) and how a `pipeline` wires them together

## Repository layout

- [`src/mhl-runtime/`](src/mhl-runtime) — the Go implementation of the `mhl` CLI (parser,
  interpreter, runtime, LSP)
- [`vscode-mhl/`](vscode-mhl) — the VS Code extension (syntax highlighting, diagnostics,
  completion), a thin wrapper around `mhl lsp`
- [`docs/`](docs) — design docs and the language reference wiki
- [`sample/`](sample) — worked `.mh` examples, doubling as the docs-facing test suite
