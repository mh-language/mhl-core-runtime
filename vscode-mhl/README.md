# MHL Language Support for VS Code

**MHL** (Meta-Harness Language, `.mh`) is a declarative language for describing
AI agent pipelines. A program declares the pieces — `agent`s, `tool`s, `memory`,
`prompt`s, MCP servers, and typed data (`type` / `enum`) — and the `pipeline`s or
`workflow`s that wire them together into an ordered run. See
[the language reference](https://mh-language.github.io/mhl-core-runtime/Docs-Reference.dc.html).

## Features

- Syntax highlighting for `.mh` files, and for MHL code blocks in Markdown
- Diagnostics as you type
- Completion and signature help
- Go-to-definition (`F12` / `Cmd`+click) across `import`s and aliases
- Find references
- An optional **MHL File Icons** theme that keeps every Seti icon and adds `.mh`

Syntax highlighting works on its own. Everything else is served by `mhl lsp`, the
language server built into the `mhl` CLI, so it needs the CLI installed.

## Requirements

Install the `mhl` CLI.

**macOS / Linux:**

```bash
curl -fsSL https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/install.ps1 | iex
```

Published binaries cover `linux-amd64`, `linux-arm64`, `darwin-arm64` (Apple
Silicon), `windows-amd64` and `windows-arm64`. There is no Intel Mac binary. Other
install options are in the
[project README](https://github.com/mh-language/mhl-core-runtime#install).

Then run **MHL: Restart Language Server** from the Command Palette.

## Settings

| Setting | Default | Description |
| --- | --- | --- |
| `mhl.serverPath` | `mhl` | Path to the `mhl` binary. `${workspaceFolder}` is expanded, e.g. `${workspaceFolder}/src/mhl-runtime/dist/mhl`. |

After changing it, run **MHL: Restart Language Server**.

## Workspace Trust

The language server only reads your files: it never runs agents, tools, or
extensions. In [Restricted Mode](https://code.visualstudio.com/docs/editing/workspaces/workspace-trust)
it keeps working, but `mhl.serverPath` comes only from your user settings and
`${workspaceFolder}` is ignored, so an untrusted folder cannot choose which binary
runs. Trusting the folder restarts the server with your full settings.

## MHL in Markdown

Use a fenced code block tagged with `mhl` (or `mh`):

````markdown
```mhl
agent Local {
    command: "echo"
}

pipeline Example {
    step Run {
        log(Local.run(prompt: "Olá"))
    }
}
```
````

The Markdown document keeps its normal highlighting, while the contents of the
fence use the same grammar as `.mh` files. This is syntax highlighting only: the
language server targets `.mh` documents, not Markdown code blocks.

## Other editors

The extension is also on the [Open VSX Registry](https://open-vsx.org/extension/mhl-language/mhl-language),
so editors that use Open VSX (VSCodium, Cursor, Windsurf, Gitpod, Eclipse Theia)
can install it by searching for **MHL Language Support** in the Extensions view.

## Development

From `vscode-mhl/` in the [repository](https://github.com/mh-language/mhl-core-runtime):

```bash
npm install
npx @vscode/vsce package   # builds mhl-language-<version>.vsix
```

Install the VSIX with **Extensions → ⋯ → Install from VSIX...**, or run
`./install.sh` to do both steps. To work on the extension, open this directory in
VS Code and press `F5` to launch an Extension Development Host. Build the language
server with `make build` in `src/mhl-runtime`.

To release, set the new version in `package.json`, add it to `CHANGELOG.md`, and
push a tag named `extensions-v<version>`.
