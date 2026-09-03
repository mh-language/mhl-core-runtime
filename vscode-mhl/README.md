# MHL Language Support for VS Code

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
