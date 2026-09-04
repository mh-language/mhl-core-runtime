# MHL Language Support for VS Code

**MHL** (Meta-Harness Language, `.mh`) is a declarative language for describing
AI agent pipelines. A program declares the pieces — `agent`s, `tool`s, `memory`,
`prompt`s, MCP servers, and typed data (`type` / `enum`) — and the `pipeline`s or
`workflow`s that wire them together into an ordered run. It is executed by the
`mhl` CLI (parser, interpreter, runtime, and language server), which can also
expose those workflows over MCP or A2A. See
[the language reference](https://mh-language.github.io/mhl-core-runtime/Docs-Reference.dc.html).

Provides syntax highlighting, diagnostics, completion, signature help, and
go-to-definition for `.mh` files. It also highlights MHL code blocks inside
Markdown files when the fence is tagged as `mhl` or `mh`.
These language features are served by `mhl lsp` (a Language Server Protocol
server built into the `mhl` binary, see `../src/mhl-runtime`) — this
extension is just its client.

Go-to-definition (`F12` / `Cmd`+click) jumps to the declaration of an
`agent` / `memory` / `tool` / `prompt` / `pipeline` / `workflow` /
`extension` / `type` / `enum` name, resolving it through the file's
`import { ... } from "..."` statements (following `as` aliases and
re-exports) and, as a fallback, any `.mh` file one directory level deep. On
a `Receiver.member` access it lands on the `tool` method or `enum` variant
named `member`; for a runtime built-in like an agent's `.run` or a memory's
`.get` it lands on `Receiver`'s declaration instead. The `"..."` path of a
`from` clause jumps to that file.

## Requirements

Build the `mhl` binary first:

```bash
cd ../src/mhl-runtime && make build
```

By default the extension looks for `mhl` on your `PATH`. If it's not there,
either symlink `src/mhl-runtime/dist/mhl` onto your `PATH`, or point the
`mhl.serverPath` setting at it directly (`${workspaceFolder}` is expanded,
e.g. `${workspaceFolder}/src/mhl-runtime/dist/mhl`). After changing it, run
**MHL: Restart Language Server** from the command palette.

## Installation

The extension is published on the [Open VSX Registry](https://open-vsx.org/extension/local-mhl/mhl-language),
so it installs directly from the Extensions view in editors that use Open VSX —
VSCodium, Cursor, Windsurf, Gitpod, Eclipse Theia — by searching for
**MHL Language Support**. It is not on the Visual Studio Marketplace; on
stock VS Code, build the VSIX as described below.

## Local installation

From this directory, install dependencies and create the VSIX package:

```bash
npm install
npx @vscode/vsce package
```

Then install it in VS Code with **Extensions → ⋯ → Install from VSIX...**
(or run `./install.sh`, which does both steps and installs it for you).

For development, run `npm install` here, then open this directory in VS
Code and press `F5` to launch an Extension Development Host.

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

The Markdown document keeps its normal Markdown highlighting, while the
contents of the MHL fence use the same grammar as `.mh` files. This is
syntax highlighting only; the MHL language server currently targets `.mh`
documents, not embedded Markdown regions.
