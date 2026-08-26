# mhl-runtime

Go implementation of the **mhl** CLI — the runtime for the Meta-Harness Language (`.mh`), a
declarative language for describing AI agent pipelines: agents, tools, memory, MCP
servers, prompts and the pipelines that wire them together.

```
mhl run <pipeline.mh> [--input key=value ...] [--resume]
mhl test <file.mh>
mhl lint [dir]
```

## Design goal of this layout

The codebase is organized so a contributor can answer *"where does this belong?"* without
reading the whole tree. Everything under `internal/` falls into exactly one of four groups:

| Group | Question it answers | Depends on |
|---|---|---|
| [`internal/lang/`](internal/lang) | What is valid `.mh` syntax, and what shape does it parse into? | nothing else in this repo |
| [`internal/engine/`](internal/engine) | How does a parsed program actually run? | `lang`, `features` |
| [`internal/features/`](internal/features) | What can a `.mh` program *do* (agents, memory, tools, MCP, ...)? | `lang/ast`, for the few whose API is expressed directly in AST terms |
| [`internal/cli/`](internal/cli) | How does a user invoke all of the above from a terminal? | `lang`, `engine`, `features` |

The dependency direction only ever flows down this table: `features` packages never import
`engine` or `cli`; `engine` imports `lang` and `features` but never `cli`; `cli` is the only
package allowed to import all three. Most `features` packages (`memory`, `tools`, `nativeops`,
`traffic`, `auth`, `adapters`) don't even import `lang` — they're plain Go APIs with no idea
`.mh` exists. A few (`prompt`, `mcp`) do import `lang/ast`, because the most natural
shape for their public function is "take the declaration node directly"
(`prompt.Render(*ast.Prompt, args)`) rather than having every
caller unpack the node into primitives first — that's still a one-way, downward dependency
(`ast` never imports them back), not a violation of the split. This is what makes each group
independently readable — you can understand `internal/features/memory` in isolation, without
pulling in how the interpreter or the CLI work.

```
cmd/mhl            entry point — main() calls internal/cli.Run
internal/
├── lang/          language core: grammar, AST, parser, static analysis (lint)
├── engine/        executes a parsed program: the interpreter + pipeline checkpointing
├── features/      the building blocks a .mh program can declare and call into
└── cli/           command-line argument parsing and dispatch
test/fixtures/     example .mh programs used by parser/interpreter tests
```

## `internal/lang` — the language itself

Everything here is about **structure and syntax**, not behavior: given `.mh` source text,
what AST does it parse into, and is it well-formed? None of these packages execute anything.

- **`ast`** — the Go types describing a parsed `.mh` program (`Program`, `Agent`,
  `Memory`, `Tool`, `MCPServer`, `Prompt`, `Pipeline`, `Test`, and the expression/statement
  grammar). Each node's [Participle](https://github.com/alecthomas/participle) struct tags *are*
  the grammar — e.g. `` `parser:"'agent' @Ident?"` `` on `Agent.Name` is simultaneously the Go
  field definition and the declaration that `agent` is a reserved keyword. This is why there
  is no separate "keywords" file: in a Participle-based parser the reserved words live next to
  the node they introduce, one per file (`agent.go`, `prompt.go`, `pipeline.go`,
  `test.go` for `test`/`describe`, `program.go` for `import`/`use`/`memory`/`tool`/`mcp_server`,
  `expr.go` for operators).
- **`parser`** — the lexer (`lexer.go`: token rules for strings, numbers, durations,
  identifiers, punctuation) and the compiled Participle parser (`parser.go`), exposing
  `Parse(source) (*ast.Program, error)` and `ParseExpr` (used for `${...}` interpolation).
- **`lint`** — static analysis over one or more `.mh` files: broken `import`/`use` targets,
  calls to undeclared agents, misconfigured agents, syntax errors — everything that would
  otherwise only surface when `mhl run` happens to reach the offending line.

`ast` additionally exposes `literal.go`: read-only helpers (`StringValue`, `NumberValue`,
`BoolValue`, `StringArrayValue`, `DurationValue`, `BareObject`, `BarePostfix`) for pulling a Go
value straight out of an `*Expr` that's just a bare literal — no evaluation, no `Env`. This is
what a declaration's config (an agent's `command:`, a memory's `type:`, a pipeline's
`checkpoint { ttl: 7d }`) is read with, everywhere that isn't the interpreter's full expression
evaluator. Several different packages independently reimplemented this exact logic before this
refactor (`internal/cli`, `internal/runtime`, `internal/lint`, `internal/prompt` and
`internal/mcp`, one copy each); it's a good example of the
language/feature boundary this layout enforces — "what counts as a bare literal" is a fact
about the AST's shape, so it lives in `lang/ast` once, and `engine/interpreter`,
`engine/runtime`, `lang/lint`, `features/prompt` and `features/mcp` all call
into that single copy now instead of keeping their own. `mcp`'s `env("KEY")`-resolution and
`"a" + "b"` string-concatenation logic is genuinely mcp-specific (fetching credentials is a
feature concern, not a language one) and stays local to that package — only the underlying
"unwrap this Expr to a bare literal" shape-matching moved.

## `internal/engine` — running a parsed program

- **`interpreter`** — the tree-walking evaluator. `RunStep` executes one pipeline step's
  statement block against a fresh variable environment (`Env`); `eval.go` evaluates
  expressions (operators, literals, member access); `closure.go` implements lambda values;
  `interpolate.go` handles `${...}` string interpolation; `tool.go` dispatches a declared
  `tool` method call and the reserved `cmd`/`git`/`fs`/`http` native-op namespaces;
  `agent.go`, `memory_ops.go` and `prompt_ops.go` dispatch `Agent.run(...)`,
  `Memory.method(...)` and a `prompt:` argument respectively; `imports.go` resolves
  `import`/`use` declarations before a program runs; `test.go`'s `RunTests` executes every
  `test { describe { ... } }` block against a fresh scope per `describe`, without touching a
  program's pipelines. A `describe` body reuses the exact same statement grammar a pipeline
  step's body does (`var`/`if`/`while`/`for-in`/assign), so a bare call to a builtin assertion
  (`are_equal`, `is_true`, `includes`, `incomplete`, ...) can appear anywhere a statement can,
  including nested inside `if`/`while`/`for-in` — `execExprStatement` (`exec.go`) is what
  recognizes and records one instead of evaluating it as an ordinary expression, so the same
  `execIf`/`execWhile`/`execForIn` a pipeline step already uses handles a describe block's
  control flow too, with no separate execution path. This package is the one place the language
  (`lang`) and the features (`features`) meet.
- **`runtime`** — pipeline checkpointing and `--resume`: `Runner.Run` executes a pipeline's
  steps in order, persisting a `Checkpoint` after each step when the pipeline declares
  `checkpoint { strategy: "per_step" }`, and resuming from the step after the last completed
  one. Independent of the interpreter — it only knows step *names*, not what a step does.

## `internal/features` — what a `.mh` program can use

Each package here backs exactly one thing a `.mh` author can declare or call, and none of
them know anything about the interpreter that drives them: no `Env`, no expression evaluation,
no notion of a running step. A few take an AST declaration node as a plain input value (see the
`lang/ast` note above), but it's the interpreter's `agent.go`/`memory_ops.go`/`prompt_ops.go`
that decide *when* and *how* to call into these packages — the packages themselves never call
back into the interpreter.

- **`prompt`** — renders a `prompt Name(params) { "..." }` template by substituting its
  `${param}` placeholders.
- **`memory`** — the storage backends a `memory` block can declare: an in-process KV store, a
  disk-persisted JSON store, an append-only text log, and an append-only JSONL log.
- **`mcp`** — a Model Context Protocol client (stdio and HTTP/SSE transports) for
  `mcp_server` declarations.
- **`nativeops`** — implements the fixed `cmd.exec`/`git.diff`/`fs.read`/`fs.write`/`http.post`/
  `json.parse` operations a `tool` method body can call. Deliberately thin and MHL-agnostic (no
  AST): the interpreter's `tool.go` is what evaluates a call's arguments and calls into this
  package.
- **`tools`** — low-level OS process execution (`Cmd.Exec`), including the process-group
  isolation used to run local subprocesses cleanly. This is the primitive both `nativeops`
  (`cmd.exec`) and `adapters` (running a CLI agent) are built on — it is unrelated to, and
  named independently of, the language's `tool` declarations (see `nativeops` for those).
- **`adapters`** — runs an agent's configured engine: a local CLI subprocess (`cli/*`) or an
  Ollama HTTP endpoint (`ollama/*`).
- **`traffic`** — agent request resiliency: retry with backoff, and response caching.
- **`auth`** — resolves credential references (`env(...)`, vaults) at the point of use and
  keeps resolved secrets out of diagnostics and persisted checkpoint state.

## `internal/cli`

Argument parsing and dispatch for the subcommands (`run`, `test`, `lint`, `lsp`, `version`).
Kept deliberately thin: it parses flags, reads files, and hands off to `lang/parser`,
`engine/interpreter`, `engine/runtime` and `lang/lint` for the actual work.
`test` prints each assertion's PASS/FAIL/SKIP and a summary line, and exits non-zero when any
assertion failed (an `incomplete(...)` assertion never counts as a failure). If you're looking
for *how* a pipeline executes, you want `internal/engine/interpreter`, not here.

## Tests

Nearly every test in `internal/cli` is a black-box, end-to-end test: it writes a `.mh`
fixture to a temp directory and drives it through `cli.Run([]string{"run", ...}, buf)`,
asserting on stdout/stderr — exercising the interpreter, the pipeline runner and the language
features together the same way a real `mhl run` invocation would. Each `internal/features/*`
and `internal/lang/*` package additionally has its own focused unit tests. Run everything with:

```
make test   # go test ./...
```
