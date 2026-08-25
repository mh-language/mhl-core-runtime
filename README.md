# mhl

**mhl** (Meta-Harness Language, `.mh`) is a declarative language for describing AI agent
pipelines: agents, skills, tools, memory, MCP servers, prompts, and the pipelines that wire
them together. See [`src/mhl-runtime`](src/mhl-runtime) for the Go implementation of the `mhl`
CLI and [`vscode-mhl`](vscode-mhl) for editor support.

The language manual is available as a Markdown wiki in [`docs/wiki`](docs/wiki/README.md).

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
