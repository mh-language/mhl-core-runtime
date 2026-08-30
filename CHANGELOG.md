# Changelog

All notable changes to **mhl** (the Meta-Harness Language and its `mhl` CLI) are
recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versioning is [semantic](https://semver.org/), tagged `vMAJOR.MINOR.PATCH-alpha`
during the alpha series.

Per-tag release notes are also generated automatically on the
[GitHub Releases](https://github.com/mh-language/mhl-core-runtime/releases) page.

## [1.0.0-alpha] — Unreleased

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

[1.0.0-alpha]: https://github.com/mh-language/mhl-core-runtime/compare/v0.5.3-alpha...HEAD
