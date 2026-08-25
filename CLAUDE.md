# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository overview

This repo is the implementation of **mhl** (Meta-Harness Language, `.mh`): a declarative
language for describing AI agent pipelines — agents, skills, tools, memory, MCP servers,
prompts, and the pipelines that wire them together. It has three tracked parts:

- **`src/mhl-runtime/`** — the Go implementation of the `mhl` CLI (parser, interpreter,
  runtime, LSP). This is where nearly all engineering work happens.
- **`vscode-mhl/`** — a VS Code extension providing syntax highlighting, diagnostics, and
  completion for `.mh` files; it's a thin `vscode-languageclient` wrapper around `mhl lsp`.
- **`docs/`** — `language-design.md` and `language-specification.md` describe the language
  surface with worked examples. Some example fields are aspirational, not yet implemented —
  see "Docs vs. implementation" below before trusting an example as ground truth.

A `dotnet/` directory may be present locally but is `.gitignore`d in its entirety — it is not
part of this repository and unrelated to `mhl`.

## Commands

All Go commands run from `src/mhl-runtime`:

```sh
make build            # builds dist/mhl and test/mhl
make test             # go test ./...
make functional-test  # runs `mhl test` against test/e2e/features and test/e2e/lang/syntax
go vet ./...
make release          # cross-compiles linux-amd64, darwin-arm64, windows-amd64
make verify-release   # release + asserts each binary is non-empty
```

Focused iteration:

```sh
go test ./internal/lang/parser/...          # one package
go test -run TestName ./...                 # one test by name, across packages
go test ./internal/cli/... -run TestName -v # one test, verbose
```

CI (`.github/workflows/ci.yml`) runs `go vet ./...`, `make build`, `make test`, then
`make functional-test`, in that order — reproduce locally before pushing.

`internal/lang/parser`'s `TestFixturesParse` fails out of the box (`test/fixtures` doesn't
exist in this checkout) — a pre-existing gap, not something a normal change will have caused.

The built CLI itself:

```sh
mhl run <pipeline.mh> [--input key=value ...] [--resume]
mhl test <file.mh|dir>
mhl skills list [dir]
mhl lint [dir]
mhl lsp        # LSP server over stdio, used by vscode-mhl
```

VS Code extension, from `vscode-mhl` (needs `mhl` built first — `mhl.serverPath` defaults to
`mhl` on `PATH`):

```sh
npm install
npm run compile   # esbuild bundle to out/extension.js
npm run watch
npx @vscode/vsce package   # .vsix
```
Open `vscode-mhl/` in VS Code and press F5 for an Extension Development Host.

## Architecture — `src/mhl-runtime`

`internal/` is split into four groups with a strict, one-way dependency order (see
`src/mhl-runtime/README.md` for the full rationale):

| Group | Answers | Depends on |
|---|---|---|
| `internal/lang/` | What is valid `.mh` syntax, and what does it parse into? | nothing else here |
| `internal/engine/` | How does a parsed program actually run? | `lang`, `features` |
| `internal/features/` | What can a `.mh` program *do*? | `lang/ast` (a few packages only) |
| `internal/cli/` | How is this invoked from a terminal? | `lang`, `engine`, `features` |

`features` packages never import `engine` or `cli`; `engine` never imports `cli`. Most
`features` packages (`memory`, `tools`, `nativeops`, `traffic`, `auth`, `adapters`) have no
idea `.mh` exists at all — they're plain Go APIs the interpreter calls into.

Key packages:

- **`internal/lang/ast`** — the parsed program's Go types. Participle struct tags on each
  node (e.g. `` `parser:"'agent' @Ident?"` ``) *are* the grammar — reserved keywords live next
  to the node they introduce (`agent.go`, `pipeline.go`, ...), not in a separate keywords file.
  `literal.go` holds the shared "unwrap a bare literal" readers (`StringValue`, `BoolValue`,
  `DurationValue`, `BareObject`, ...) that every declaration's static config (an agent's
  `command:`, a pipeline's `checkpoint { ttl: 7d }`) is read through outside the interpreter's
  full expression evaluator.
- **`internal/lang/parser`** — lexer + compiled Participle parser (`Parse`/`ParseExpr`).
- **`internal/lang/lint`** — static analysis (broken imports, undeclared agents, misconfigured
  config) shared by `mhl lint` and diagnostics published by the LSP.
- **`internal/engine/interpreter`** — the tree-walking evaluator. `eval.go` is expressions;
  `exec.go`/`RunStep` is statements; `agent.go`/`memory_ops.go`/`prompt_ops.go` dispatch
  `Agent.run(...)`/`Memory.method(...)`/prompt rendering; `tool.go`'s `nativeOpCall` dispatches
  the reserved native namespaces (`cmd`, `git`, `fs`, `http`, `json`, `log`) a `tool` method
  body or any expression position can call. This is the one package where `lang` and
  `features` meet.
- **`internal/engine/runtime`** — pipeline execution order, `loop pipeline`'s repeat policy,
  and checkpoint persistence for `--resume`. Knows step *names* only, not step behavior.
- **`internal/features/*`** — `prompt`, `skills`, `memory` (kv/json/log/jsonl backends),
  `mcp`, `nativeops` (the actual `cmd.exec`/`fs.read`/... implementations behind `tool.go`),
  `tools` (low-level subprocess exec), `adapters` (runs an agent's `cli/*` or `ollama/*`
  engine), `traffic` (retry/backoff, response caching), `auth` (credential resolution).
- **`internal/cli`** — argument parsing/dispatch only; hands off immediately to the packages
  above.
- **`internal/lsp`** — the `mhl lsp` server. Completion (`completion.go`, `blockcontext.go`,
  `properties.go`) and symbol discovery (`symbols.go`) **duplicate the interpreter's knowledge
  by hand** — there is no shared source of truth. When adding/changing a native op
  (`tool.go`'s `nativeOpCall`) or a declaration property (`agent.go`, `runtime/pipeline.go`),
  update the matching LSP tables too, or completion will silently drift out of sync.

### Docs vs. implementation

`docs/language-design.md` and `docs/language-specification.md` show aspirational examples that
outrun what's actually wired up. For agents specifically, `internal/engine/interpreter/agent.go`
only reads `engine`, `command`, `args`, `endpoint`, `temperature`, `log`, `trace`, `retry`,
`cache`, `rate_limit`, `fallback` — fields the docs also show (`api_key`, `skills`,
`mcp_servers`, `tools`, `timeout`, `system_instructions`) are not read anywhere in `agent.go`
and are silently ignored if written. Some accepted nested fields are read but only partially
honored — `agent.go` marks these inline as `SKETCH GAP` (e.g. `cache.strategy` is accepted but
only exact-match caching is implemented; `retry.backoff` is accepted but always exponential).
Before relying on a docs example, grep the relevant `internal/engine/interpreter` or
`internal/features/nativeops` file for the exact property/op name.

## Testing conventions

- `internal/cli` tests are largely black-box, end-to-end: write `.mh` source to a temp dir,
  run it through `cli.Run([]string{"run", ...}, buf)`, assert on stdout — exercising the
  parser, interpreter, and runner together the way a real `mhl run` would.
- `internal/features/*` and `internal/lang/*` additionally have focused unit tests beside the
  code they cover.
- `test/e2e/features`, `test/e2e/lang/syntax`, and `test/e2e/scenarios` hold real `.mh`
  programs exercised via `mhl test`/`mhl lint`, not `go test`; `make functional-test` runs the
  first two directories through the built `test/mhl` binary.
- Prefer table-driven cases for parser/runtime behavior.

## Coding style

Idiomatic `gofmt`-formatted Go, tabs, documented exported identifiers, small package-focused
changes. Lowercase package/file names; `snake_case` only where an existing filename already
requires it. Conventional Commit messages (`feat(scope): ...`, `fix(scope): ...`,
`test(scope): ...`).
