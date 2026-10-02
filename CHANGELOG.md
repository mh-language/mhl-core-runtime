# Changelog

All notable changes to **mhl** (the Meta-Harness Language and its `mhl` CLI) are
recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versioning is [semantic](https://semver.org/), with prerelease suffixes while a
release is being stabilized.

Per-tag release notes are also generated automatically on the
[GitHub Releases](https://github.com/mh-language/mhl-core-runtime/releases) page.

## Unreleased

### Upgrade notes

- **`extensible` method signatures declare their return type with `:`.**
  Write `get(key: string): any`, exactly like a tool method, instead of
  `get(key: string) -> any`. `->` now always introduces a body, never a type.
  The old form still loads, so installed extensions keep working, but
  `mhl lint` rejects it.

### Changed

- **`->` is optional before a tool method's block body.**
  `count(items): number { ... }` is the same as `count(items): number -> { ... }`;
  the `{` already marks where the body starts. `->` is still required before an
  expression body, and `-> { key: value }` is still an object literal.

## 1.5.0-alpha.1

This release is about writing less `.mh` for the same program: the language
gains the constructs real workflows kept spelling out by hand (enums that work
as inputs, JSON Schema files, one flow published under several names, private
helpers, shorthand fields and arguments, destructuring), and `mhl lint` now
catches several mistakes that previously only failed — or silently did
nothing — at run time.

### Upgrade notes

Read these before upgrading; each can change the behavior of a program that ran
under 1.4.

- **Inputs are read-only.** Assigning to a pipeline/workflow `input` (`x = …`,
  `x += …`, `self.x = …`, a destructuring target) or redeclaring it with `var`
  is now an error in `mhl lint` and at run time. Such a write never worked: the
  runtime re-binds inputs before every step, so the new value was silently
  undone at the next one (a gate that "cleared" an input to leave a `goto`
  loop re-entered it forever). Move the changing value into a `var`.
- **Enum-typed inputs are converted and checked.** An `input x: SomeEnum`
  receiving text (`--input x=a`, an MCP/A2A JSON string, `Name.run(inputs:)`)
  now gets the enum value; a name that is not a variant is rejected as an
  invalid input before the run starts. Under `Name.run(...)` in tests that is a
  raised error, not `{ok: false}` — wrap such assertions in `try`/`catch`.
  Code comparing an enum input with a string (`x == "a"`) was always false
  and is now a lint error; compare with `SomeEnum.a`.
- **`mhl serve` publishes only what each file declares.** A pipeline/workflow
  that a file merely imports is no longer published again from that file. This
  removes a spurious "declared in more than one file" error for a shared
  workflow imported by several files; a workflow imported from *outside* the
  served directory is no longer published at all. Declarations marked
  `internal` are never published.
- **`mhl run file.mh` skips `internal` declarations** when choosing which
  pipeline to run, and errors when a file declares only internal ones.
- **Checkpoint format version 3.** Enum values are now checkpointed as enums.
  Checkpoints written by 1.4 still resume under 1.5; a checkpoint written by
  1.5 cannot be resumed by 1.4.
- **New lint errors.** Code that linted clean under 1.4 may now report: an
  undefined receiver (`Missing.pick()`), an unknown name or unparseable
  expression inside `${...}` (e.g. `catch { log("${e}") }` without `(e)`), a
  lifecycle hook parameter annotated with the wrong context type, an enum
  compared with a string literal, and a call to another tool's `internal`
  method. Each was already a runtime failure or a silent bug.
- **`+` between a string and a scalar now concatenates.** `"n=" + 3` is
  `"n=3"` (number, bool or enum value, either order) instead of raising. A
  string with `null`, an object or an array still raises, with a new message.
- **`schema` and `internal` are new keywords**, recognized only where they
  start a declaration or prefix a method; existing identifiers with these
  names keep working. A lambda whose block body is a single bare identifier,
  `(x) -> { y }`, now parses as the object `{y: y}` (it used to be a block
  that returned nothing).

### Added

- **Workflow aliases.** `workflow Delivery = ArtifactFlow with { level:
  Level.delivery } { description: "…" }` publishes an existing workflow under
  another name with some inputs fixed. It runs the target's steps unchanged
  but is its own entry point everywhere — MCP tool / A2A skill, `mhl run`,
  `Delivery.run(...)` in tests, sessions, checkpoints, `mem` state — and its
  input contract is the target's minus the bound inputs, which a caller can no
  longer pass. Bound values are literals or enum values, checked statically.
- **`internal` modifier.** `internal workflow X` (also `pipeline` and aliases)
  is not an entry point — not published by `mhl serve`, not picked by
  `mhl run` — but still runs as an alias target and from tests; the typical
  shape is a shared flow published only through its aliases. On a tool method,
  `internal name(...)` makes it private: callable as `self.name(...)` and from
  the tests of the file that declares the tool, a lint and runtime error
  anywhere else, and hidden from `Tool.` completion.
- **`schema` declarations.** `schema Brief from "schemas/brief.schema.json"`
  evaluates to `{content, path}` — the JSON text (for an engine taking the
  schema inline, e.g. `claude --json-schema`) and the file's absolute path
  (for one taking a file, e.g. `codex --output-schema`, which resolves
  relative paths against its own working directory). A missing file, invalid
  JSON or a non-object document is a lint and load-time error. The checkpoint
  digest covers the schema's content, not its path.
- **Enums end to end.** Besides input conversion (above): `Kind.parse(text)`
  returns the variant named by `text` (raising with the variant list) and
  `Kind.values()` lists every variant; both work even when an enum declares a
  variant named `parse` or `values`.
- **Shorthand fields and arguments.** `{artifact, path}` is `{artifact:
  artifact, path: path}`, and `f(project_id:)` is `f(project_id: project_id)`.
  Expanded by the parser, so evaluation, lint and the checkpoint digest are
  identical to the long form.
- **Destructuring.** `var {data, revisao: note} = r` declares locals,
  `{a, b} = r` assigns existing names, `self.{tokens_in, tokens_out} = r`
  assigns the pipeline's vars. The right-hand side is evaluated once; a missing
  field raises like `r.field`.
- **`dir.clear(path)`** leaves `path` as an existing, empty directory (created
  when absent, contents removed at any depth when present) — for regenerating
  a whole collection. It refuses an empty path, a filesystem root, the working
  directory or one of its ancestors, and a symlinked path. `dir.delete` stays
  non-recursive.
- **Typed workflow signatures.** `workflow Review(req: ReviewInput):
  ReviewOutput` binds the inputs as one read-only object, supports optional
  fields (`base?: string`), requires an explicit `output:` checked against the
  result type, and advertises the result as the MCP `outputSchema`.
- **Reference objects.** `ref { ... }` creates an object with identity that is
  shared — not copied — between names, including across `parallel` branches
  and checkpoint/resume; plain objects and arrays remain values.
- **`with` expressions.** `obj with { field: value }` returns a copy of `obj`
  with the listed fields replaced.
- **`array.enumerate()`** returns `{index, value}` pairs for indexed iteration.
- **`Router.select(prompt: ...)`** is a public method: it runs the router's
  deterministic `select` hook and returns the chosen agent's name (or `null`)
  without running it.
- **LSP:** completion of `Kind.parse`/`Kind.values` and enum variants, schema
  fields (`Brief.content` / `.path`), workflow aliases as symbols, the
  `internal` and `schema` keywords; file and import caching for faster symbol
  resolution.

### Changed

- Lint and the language server share the new checks above, so the editor
  reports them as you type.
- `mhl serve`'s workflow manifest resource (`mhl://workflow/<name>`) lists the
  program's declared `schemas` alongside its agents, tools, prompts and
  extensions.
- Move package-specific extension fixtures and end-to-end tests to the
  `mh-language/mhl-packages` repository. The core keeps its generic extension
  creation, installation, protocol and interpretation coverage.
- The release script can overwrite already-uploaded artifacts when a release
  upload is retried.

### Fixed

- Two pipelines in one program that declared steps with the same name (common
  once a file imports a shared workflow) could run each other's step bodies;
  steps are now resolved within the declaration being run.
- An enum value held in a pipeline `var` lost its type across a checkpoint and
  came back as a plain string.
- `self.<name>` inside a pipeline lifecycle hook now resolves the pipeline's own
  input/var/mem.
- Language server: hook-parameter completion nested inside `try`/`if`/object
  literals; tool-method completion while editing, which ignored every method
  declaring a return type.
- Redact registered secrets recursively—including dynamic object keys—from
  structured `pause` reasons and from `break` reasons in text, JSON and MCP
  status output; checkpointed secret keys remain rehydratable.
- Include `break_reason` in `mhl run --format json`, matching the workflow-run
  result contract.
- Recognize `remove(key)` as a valid JSON-memory operation in `mhl lint`, with
  the same argument validation enforced by the runtime.
- VS Code extension: update `brace-expansion` to 2.1.7 (security advisories
  GHSA-q2hr-2g5m-vwhr, GHSA-qhr7-859c-m2p7, GHSA-6j4f-fj2g-mc7p).

## 1.4.0-beta — v1.4.0-beta.2 .. v1.4.0-beta.24 (2026-09-06 → 2026-09-22)

Everything below shipped across the `v1.4.0-beta.2` .. `v1.4.0-beta.24` tags —
grouped by theme rather than per-beta, since the betas themselves are cut
frequently and each carries `docs/site`/CI fixes alongside features. See the
[GitHub Releases](https://github.com/mh-language/mhl-core-runtime/releases)
page for the exact per-tag notes.

### Added

- **Pipeline/workflow lifecycle hooks.** `session_start`, `session_end`,
  `step_start`, `step_end`, `stop_failure` — five single-parameter lambda body
  properties, each bound to a builtin global type (`SessionContext` /
  `StepContext` / `FailureContext`). Observation-only: a non-nil return is a
  runtime error, and a `stop_failure` failure is only ever logged, never
  allowed to replace the failure it was reporting.
- **Partial pipelines/workflows.** `partial` + `entry step` + a bare `import`
  let a single pipeline/workflow definition be split across files, with
  `complete()` closing a partial fragment explicitly (removing the old
  fallthrough footgun) and `self.name` reading/writing the pipeline's own
  input/var/mem scope regardless of which fragment is executing.
- **`router` declarations.** `router <Name> { agents: […], select: (prompt) ->
  {…}, decider: … }` — a hybrid decision between a deterministic `select` hook
  and an LLM decision call through a separately declared `agent`, with
  `.delegate(prompt: …)` and `nameof(...)`-based typo-safe, IDE-navigable
  agent references.
- **`goto match` for static step dispatch**, and stricter lint diagnostics
  around `goto` cycles and ambiguous `return`/`break` (a bare `return`/`break`
  followed by more code — even a guard clause — is now a parse error instead
  of silently swallowing the next statement).
- **Skills support and scoped declarations.**
- **`stdin` property for agents**, for handling large content without
  round-tripping it through a rendered prompt string.
- **`os` native namespace**: user, home directory, hostname, platform,
  architecture, current working directory, process ID, and an `executable`
  method — plus a `dev-install` script.
- **Nested string literals inside `${...}` interpolation** — an unescaped
  string literal nested in an interpolation expression now parses correctly.
- **LSP: "Find References" and "Go to Definition"**, with caching for
  imported-symbol resolution across partial fragments.
- **Completion snippets** for full agent/pipeline/workflow declarations, and
  general declaration/snippet-syntax completion items.
- The four official extensions (`mhl-store-s3`, `mhl-store-postgres`,
  `mhl-sql-postgres`, `mhl-cache-redis`) moved out of this repository
  (`src/mhl-extensions/`) into a separate `mhl-packages` repository — an
  organizational change, not a behavior change for a project already
  depending on one of them via `mhl extension install`.

### Changed

- Session handling hardened for concurrent requests (explicit getters instead
  of shared mutable access), and test reliability improved around it.
- Pipeline input handling and several security-relevant paths hardened
  (credential/secret handling, request guards).
- CI trigger fixes and improved runtime-release resolution for the
  installers.

## 1.2.0-alpha — 2026-08-30 → 2026-09-19

### Breaking

- **The module-import keyword is now `import`, not `use`.** The grammar is
  `import { Name [as Alias], ... } from "file.mh"` — semantics, transitive
  resolution, and `export` gating are unchanged. Existing `.mh` files must
  rename `use { ... } from "..."` to `import { ... } from "..."`; `use` is no
  longer recognised.

### Added

- **Safe uninstall scripts for macOS/Linux and Windows.** `uninstall.sh` and
  `uninstall.ps1` remove the runtime, installer-managed `PATH` entry, and VS
  Code extension. User-wide extensions and project state are preserved unless
  the explicit `--purge` / `-Purge` option is supplied.
- **`mhl serve mcp --http [--addr host:port] [--token t] [dir]`.** The MCP
  server over the Streamable HTTP transport, for clients that connect over
  the network rather than spawning the process: one JSON-RPC message per
  `POST /mcp`, `application/json` responses only (no SSE). Both protocol
  modes are accepted — the standard lifecycle (`initialize` issues an
  `Mcp-Session-Id` header the client echoes on every later request;
  `DELETE /mcp` ends the session; an unknown session is `404`) and the
  stateless `2026-07-28` form (`params._meta` on every request, no session).
  Defaults to `127.0.0.1:8711`; `--token` / `MHL_SERVE_TOKEN` enables
  `Authorization: Bearer` enforcement and the `Origin` header, when sent,
  must be loopback. `GET /mcp` is `405` (no server-to-client stream). The
  stdio transport (`mhl serve mcp`) is unchanged. The request context is the
  client connection, so a disconnect cancels an in-flight run.
- **Async workflow execution over HTTP: `run/start`, `run/status`,
  `run/resume`, `run/cancel`, `run/list`.** An extension to the HTTP MCP
  server for workflows too long to hold a `tools/call` connection open for.
  `run/start` takes the same `{name, arguments}` as `tools/call` and returns
  a `runId` immediately; `run/status` reports `state`
  (`working`/`completed`/`failed`/`canceled`), the current `step` with
  `stepIndex`/`stepTotal`, the ordered `reached` steps, `resumable`, and —
  once complete — `vars`; `run/cancel` stops one. Runs are gated by the same
  protocol context as `tools/*`, descend from the server lifetime (so
  shutdown cancels them), and terminal runs are kept for an hour for a late
  poll. Each run is **owned by the session that started it**: `run/status`,
  `run/resume`, `run/cancel` and `run/list` only act for that caller (any
  other sees "unknown runId"); stateless callers, having no session, share
  one anonymous owner. stdio and the synchronous `tools/call` path are
  unchanged.
- **`run/resume` and `mhl serve mcp --http --state-dir <path>`.** A run
  whose workflow declares `checkpoint { strategy: "per_step" }` and stops at
  a failing step keeps its checkpoint; `run/resume {runId, arguments?}`
  continues it from that step, merging `arguments` over the originals (where
  an approval decision goes — the human-in-the-loop pattern is a gate step
  that calls `fail("awaiting approval")`). With `--state-dir` /
  `MHL_SERVE_STATE_DIR` the run state is persistent, so `run/status` and
  `run/resume` work for a `runId` a **later process** never started;
  without it, run state is per-process and lost on restart.
- **`spawn` fan-out: `spawn xs = Agent.run(...) for item in <array>`.** A
  trailing `for <var> in <expr>` clause on a `spawn` starts one background
  agent call per element of the array `<expr>`, with `<var>` bound to that
  element while each call's arguments are built — so every call can carry a
  distinct prompt. The bound name holds an array of task handles in element
  order: it indexes (`xs[0].result`), iterates (`for (var h in xs) …`), and
  reports `xs.size()` like any array, and `wait xs` / `wait any xs` /
  `wait N of xs` expands it to its elements. A non-array iterable is a
  runtime error; an empty array yields an empty handle array that a plain
  `wait` no-ops on. The run-wide `spawn: { max_concurrency: N }` ceiling
  still bounds how many calls are in flight.
- **`uuid` native namespace: `uuid.v4()` and `uuid.v7()`.** Both return an
  RFC 9562 UUID as its canonical 36-character lowercase string. `v4` is fully
  random; `v7` prefixes a 48-bit Unix-epoch millisecond timestamp, so values
  minted in sequence sort in creation order. All non-fixed bits come from
  `crypto/rand`; an entropy failure raises like any other native-op error. No
  new dependency — the runtime still builds on participle alone.

## 1.1.0-alpha — 2026-08-30

Serving workflows to other agents, and the run-core work that enables it. The
language surface is unchanged from `1.0.0-alpha`.

### Added

- **`mhl serve mcp <dir>` / `mhl serve a2a <dir>`.** Expose every
  pipeline/workflow declared under a directory to another agent — as Model
  Context Protocol tools over stdio JSON-RPC, or as Agent2Agent (A2A 0.2)
  skills over HTTP (Agent Card at `/.well-known/agent-card.json` and
  `/.well-known/agent.json`, `message/send` / `tasks/get` / `tasks/cancel`,
  `A2A-Version` header, `configuration.blocking`, `-32002` on a non-cancelable
  task). The tool/skill input contract is a JSON Schema derived from each
  workflow's `input name: Type` declarations; a call runs it in a throwaway
  state directory and returns the final variable state.
  The MCP server is dual-era: an `initialize` request selects the legacy
  handshake (revisions 2025-11-25 / 2025-06-18 / 2025-03-26, negotiated);
  otherwise the connection follows the stateless 2026-07-28 revision —
  `params._meta` protocol context required on every `tools/*` request
  (missing → -32602, unsupported `protocolVersion` → -32022), `server/discover`
  in place of `initialize`, every result with `resultType: "complete"` and
  `_meta.io.modelcontextprotocol/serverInfo`, `structuredContent` on
  `tools/call`, and `ttlMs` / `cacheScope` on `tools/list` and
  `server/discover`. `ping` is served only to legacy clients; a running
  tool's `log()` output goes to stderr, never the protocol stream.
- **`description: "..."` on a `pipeline` / `workflow`.** An optional body
  property, surfaced as the MCP tool / A2A skill description by `mhl serve`
  (a generic string when absent). `mhl lint` now also rejects an unknown
  property in a pipeline/workflow body (`checkpont:`, a docs-only field),
  matching the agent-body check.
- **Cancellable runs.** Pipeline execution now threads a `context.Context`:
  cancel / deadline takes effect at step and loop-iteration boundaries and
  inside a blocking `cmd`/`git`/`http` native op or agent call — what
  `tasks/cancel` and a server request timeout use.

## 1.0.1-alpha — 2026-08-30

### Added

- **`${...}` interpolation in a `memory` block's `path:`.** The same mechanism
  an agent's `log:` path already used — `path: ".mhl/s.${context.session_id}.json"`
  gives each run its own store.

## 1.0.0-alpha — 2026-08-29

First tag of the **language-surface freeze**: the grammar, standard library, and
execution semantics are the contract from here on. External integrations
(extensions, engine adapters, MCP/A2A protocol revisions) keep evolving.

### Breaking

- **Extensions replace `mcp_server` / `a2a_agent`.** MCP and A2A are now the two
  built-in kinds of a generic capability provider: `extension mcp <Name> { … }`
  and `extension a2a <Name> { … }`. The old top-level `mcp_server` / `a2a_agent`
  keywords and their AST nodes are removed.
- **`pipeline` vs `workflow`.** `pipeline` runs its steps in declared order, each
  once — `goto` in a `pipeline` is now a lint error. `workflow` is identical in
  every other way but permits `goto <step>` (forward or backward). Both accept the
  `loop` prefix. A pre-1.0 `pipeline` that used `goto` becomes a `workflow`.
- **`import "file.mh" as alias` removed.** `use { Names [as Alias] } from
  "file.mh"` is the only cross-file mechanism; it merges the target file's
  declarations into the program's flat namespace.
- **Agent scope properties removed.** `agent { tools: […], mcp_servers: […] }` no
  longer exists. An agent reaches a tool or an extension by calling it from a
  `before:` / `after:` hook, which run real calls through mhl itself.

### Added

- **External extensions.** A language-agnostic adapter runs as a persistent
  subprocess speaking newline-delimited JSON-RPC (`initialize` / `call` /
  `shutdown`, plus inbound `log` / `secret.resolve`). Ships with an
  `extension.json` manifest format, a `.mhl/extensions.lock` allow-list, and
  `mhl extension list | doctor | init | test | package | install`. See
  `docs/site/Docs-Extensions.dc.html`; official packages and their integration
  suites live in `mh-language/mhl-packages`.
- **`goto` target validation.** `mhl lint` now reports a `goto` whose target is
  not a step of the same `workflow`, instead of it only failing at run time.
- **Unknown agent-property lint.** `mhl lint` rejects any `agent { … }` property
  the runtime does not read (e.g. `api_key`, `timeout`, `system_instructions`, or
  a typo), rather than silently ignoring it.

### Changed

- `internal/features/{mcp,a2a}` are now in-process adapters implementing the
  dependency-free `internal/extension` contract; the interpreter no longer imports
  either feature package directly.
- Documentation: the language reference gains an `extension` section framing the
  `extension <kind> <Name>` form; the `pipeline` section documents `workflow`.

## 0.1.0-alpha – 0.5.3-alpha — 2026-08-26 → 2026-08-29

The alpha bring-up series. Capabilities that landed across these tags:

- **Core language** — declarations (`agent`, `prompt`, `memory`, `tool`,
  `pipeline`, `test`), the expression and statement grammar, string interpolation,
  `type` aliases, C-style `enum` + `match` with lint exhaustiveness, a real
  `const` keyword.
- **Operators** — null-coalescing `??`, optional chaining `?.` / `?.[key]`,
  compound assignment `+=`, array collection ops (`map`/`reduce`/`filter`/…),
  object `get`, `equals`, `type_of` / `is_*`.
- **Execution** — pipeline checkpointing and `--resume`, per-execution
  `.mhl/state/<session>/` directories with `--session`, the read-only pipeline
  `context:` accessor, `mem` persistent pipeline variables.
- **Concurrency** — `spawn` / `wait any|N of` for background agent calls, and
  `parallel <Name> { step … }` step groups with an atomic checkpoint.
- **Native operations** — `cmd`, `git`, `fs`, `http` (all verbs + download),
  `json`, `log`, `time`.
- **Agents** — `cli/*` and `ollama/*` engines, `retry` / `cache` / `rate_limit` /
  `fallback` policies, `before:` / `after:` hooks.
- **MCP / A2A** — a Model Context Protocol client (stdio + HTTP, protocol
  negotiation) and an Agent2Agent client (JSON-RPC 0.2.x).
- **Tooling** — `mhl lsp` (completion, diagnostics, signature help), the
  `vscode-mhl` extension, `mhl init` / `run` / `test` / `lint`.

Compare links for the alpha-era entries above are omitted: none of those
`-alpha` tags were ever pushed (only the `v1.4.0-beta.*` series is), so a
`.../compare/vX.Y.Z-alpha...` URL 404s. For the current beta series, compare
the actual tags directly: https://github.com/mh-language/mhl-core-runtime/compare/v1.4.0-beta.2...v1.4.0-beta.24
