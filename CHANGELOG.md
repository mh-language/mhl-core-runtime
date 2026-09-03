# Changelog

All notable changes to **mhl** (the Meta-Harness Language and its `mhl` CLI) are
recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versioning is [semantic](https://semver.org/), tagged `vMAJOR.MINOR.PATCH-alpha`
during the alpha series.

Per-tag release notes are also generated automatically on the
[GitHub Releases](https://github.com/mh-language/mhl-core-runtime/releases) page.

## [1.2.0-alpha] — Unreleased

### Breaking

- **The module-import keyword is now `import`, not `use`.** The grammar is
  `import { Name [as Alias], ... } from "file.mh"` — semantics, transitive
  resolution, and `export` gating are unchanged. Existing `.mh` files must
  rename `use { ... } from "..."` to `import { ... } from "..."`; `use` is no
  longer recognised.

### Added

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

## [1.1.0-alpha] — 2026-08-30

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

## [1.0.1-alpha] — 2026-08-30

### Added

- **`${...}` interpolation in a `memory` block's `path:`.** The same mechanism
  an agent's `log:` path already used — `path: ".mhl/s.${context.session_id}.json"`
  gives each run its own store.

## [1.0.0-alpha] — 2026-08-29

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
  `docs/site/extensions.html` and `docs/extension-protocol.md`.
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

## [0.1.0-alpha] – [0.5.3-alpha] — 2026-08-26 → 2026-08-29

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

[1.1.0-alpha]: https://github.com/mh-language/mhl-core-runtime/compare/v1.0.1-alpha...HEAD
[1.0.1-alpha]: https://github.com/mh-language/mhl-core-runtime/compare/v1.0.0-alpha...v1.0.1-alpha
[1.0.0-alpha]: https://github.com/mh-language/mhl-core-runtime/compare/v0.5.3-alpha...v1.0.0-alpha
