# Changelog

All notable changes to **mhl** (the Meta-Harness Language and its `mhl` CLI) are
recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versioning is [semantic](https://semver.org/), with prerelease suffixes while a
release is being stabilized.

Per-tag release notes are also generated automatically on the
[GitHub Releases](https://github.com/mh-language/mhl-core-runtime/releases) page.

## Unreleased

## 1.16.0 — 2026-11-25

### Added

- **Prompt variants per harness/model.** A `prompt` body may list one `default` arm and any
  number of `for "<pattern>"` arms, each inline (`{ "..." }`) or `from "file.md"`. The agent
  that receives the prompt picks the arm in its own declaration, so call sites never change:
  `stdin: prompt.target("claude/" + model)`, or the literal `"${prompt.target(model)}"`
  (an input or a string literal). Inside the expressions of an agent's `command`/`args`/
  `stdin`/`cwd`/`log`/`timeout`, `prompt` is the call's prompt. An exact pattern beats a
  prefix ending in `*`, and a longer prefix beats a shorter one; with no match the agent gets
  `default`. `"${prompt}"` is still the default text, so existing agents are unchanged.
  - A rendered prompt is still a plain string (its default text). It keeps its variants
    across vars, tool parameters typed `string`, returns and the steps of a run. A string
    operation gives an ordinary string.
  - A nested prompt follows the same target. Each `fallback` leg picks its own variant. A
    router's `select` sees the default.
  - With `trace: true`, the agent prints the pick
    (`prompt Review → for "claude/*" (target "claude/sonnet")`), and the cache key tells the
    variants apart.
  - The pick is not checkpointed: after a resume, a prompt rendered before the pause gets
    its default.
  - New string method `.target(t)`. `mhl lint` checks every arm's placeholders and arm files,
    and the `${prompt.target(X)}` argument. `mhl graph` lists a prompt's `targets`. The LSP
    hover lists the variants and offers a variant snippet.
  - Programs without variants keep their `DefinitionDigest`, so their checkpoints still
    resume.
  - Design: `docs/MHL-PROMPT-VARIANTS.md`.
- **A concurrency limit on the `spawn` statement.** `spawn checks concurrency 2 =
  Echo.run(prompt: "check ${file}") for file in files` keeps at most two of that fan-out's
  calls running at once and starts the next one when a call finishes. `N` is a positive
  integer literal. Each run of the statement gets its own limit.
  - A spawn without the clause still shares the run-wide limit of 4. A statement's own
    `concurrency` is not capped by that default, so `concurrency 8` runs 8 at once.
  - `mhl lint` reports a non-positive or fractional `N` as an error. The clause on a single
    spawn (no `for`) is a warning, because it has no effect there.
  - `concurrency` is a keyword only in this position, so a handle may still be named
    `concurrency`. Programs that don't use the clause keep their `DefinitionDigest`.
- **`wait` options are clauses, without a colon.** `wait checks timeout 2s collect` is the
  new spelling of `wait checks timeout: 2s on_error: "collect"`, the same shape as a step's
  `timeout 3m`. `collect` keeps the step green and leaves each outcome on its handle.
  - `collect` is an option only when the next token can't continue a statement, so
    `collect = 1` on the line after a `wait` is still its own statement.
  - `mhl lint` now checks the options as the runtime does: `timeout` takes a duration
    literal, and `collect` only works on a plain `wait` (not `wait any` / `wait N of`).

### Deprecated

- **`spawn: { max_concurrency: N }` in a pipeline/workflow body.** Put the limit on the
  statement instead: `spawn xs concurrency N = ...`. `mhl lint` and the editor report the
  block as a warning. It still works as before: it sets the run-wide limit, and when it is
  declared it also caps statements that have their own `concurrency`.
- **`timeout: <duration>` and `on_error: "collect"` on `wait`.** Write `timeout 30s` and
  `collect`. Both old spellings still work and keep the `DefinitionDigest`, and `mhl lint`
  reports them as warnings.

### Fixed

- **`mhl lint` no longer reports a defaulted prompt parameter as missing.** It used to report
  `Echo.run(prompt: G(name: "a"))`, where `G` declares `lang: string = "en"`, as
  `missing value for parameter "lang"`, although the runtime fills in the default.

## 1.15.0 — 2026-11-22

### Added

- **An input of the wrong type fails as `invalid_inputs`, naming it.** Examples are
  `--input cmd=ls` for a `string[]` (which takes JSON, `cmd='["ls"]'`) or `n=three` for a
  `number`. The failure is `kind: "invalid_inputs"` with `invalid: ["cmd"]` and a hint on
  the form the value takes. Before, it was `kind: "other"`. On a resume, nothing ran, so the
  failure adds `resumable: true`, plus `stage` for a run paused on entering one. The MCP
  `tools/call` error's `structuredContent` carries the same. The message is unchanged
  (`runtime.InputValueError` wraps it).

- **An enum input may be given qualified.** `--input mode=SpecificationMode.Draft` is
  accepted like `mode=Draft`, which is how a default or a program writes the variant.
  This holds at any depth and through MCP/A2A arguments. Another enum's qualifier is
  still rejected, listing the variants.

- **`mhl run --no-cache` calls every agent again.** The run does not return a reply from an
  agent's `cache:`. Each fresh reply replaces the cached one, so a later run reuses that.
  Before, starting over after an agent's bad answer got the same answer back until the
  cache expired. Also `execsvc.Request.NoCache`.

- **A `///` doc above a typed gate documents its decision.** Put it over the statement that
  binds the gate, e.g. `/// approved: true publishes it` above `var approved = pause(...,
  decision: bool)`. The bound name becomes `awaiting.name` and the doc becomes
  `awaiting.description` and the decision schema's `description`. These appear in
  `--format json`/jsonl, MCP `run/status` and paused `tools/call`, and `Name.run()` in
  tests. They are kept in the checkpoint. Text output adds a `decision:` line. Like an
  input's doc, it never changes the definition digest.

- **A stage can ask for its own inputs.** An `input` line in a `partial workflow X stage S`
  fragment belongs to stage S. A run is admitted with the workflow's own inputs only. On
  entering S with a required stage input still unbound (at its first step, or a step a
  `goto` lands on), the run pauses before that step and reports `awaiting_inputs`
  (`{stage, step, inputs, schema}`). This appears in `--format json`/`jsonl`, `run/status`,
  a paused `tools/call` and `X.run(...)` in tests.
  - Resume with only that stage's inputs: `--resume --input k=v`, or MCP
    `run/resume {runId, arguments}`.
  - A resume still missing one is refused before anything runs (`invalid_inputs`, with
    `stage`), and the run stays paused. So is one that carries a `--decision`.
  - Inputs given up front never pause. Neither does a stage input with a default.
  - In `inputSchema` a stage input is optional and marked `"x-mhl-stage"`. A staged
    workflow's schema also carries the order a run asks for its inputs, so a client can guide
    a caller through it:
    - `"x-mhl-order"`: every input, in declaration order;
    - `"x-mhl-stages"`: each stage in run order, with the inputs it asks for.
  - `--dry-run`, `mhl graph` (`inputs[].stage`) and the `mhl://workflow` manifest name a stage
    input's stage.
  - A2A, which cannot resume a task, requires stage inputs up front.
  - Persisted as `Checkpoint.input_gate`, separate from a typed gate's `gate`.
  - Sample: `sample/syntax/stages/stage_inputs_pause_at_entry.mh`.
- **`base.name` reads and writes the base fragment's state in a `partial` workflow.** The
  base fragment is the one holding the `entry step`/`entry route`.
  - **Meaning.** `base.x` runs exactly like `self.x` and says where `x` is declared. In a
    stage file, `Specs.same(base.project_folder, base.project)` shows both come from the
    base.
  - **Shorthand.** It works in the object shorthand, keyed by the member:
    `{base.story_id, base.project}` is `{story_id: base.story_id, project: base.project}`.
    `{self.x}` works too.
  - **Destructuring.** `base.{plan, cost: price} = offer` assigns `base.plan` and
    `base.price` from one object, like `self.{...} = ...`.
  - **Lint.** `mhl lint` checks it:
    - `base.x` where the base declares no `x`, or `base.x` outside a `partial` workflow, is
      an error;
    - another fragment using a base name bare, or as `self.x`, gets a warning suggesting
      `base.x`. A name the fragment or step declares itself is never flagged.
  - **Not reserved.** `base` stays usable as a name: with a variable `base` in scope,
    `base.x` is its field.
  - **Editor.** `base.` completes the base's names, and hover and go-to-definition on
    `base.x` lead to its declaration. The VS Code grammar highlights it.
  - **Samples.** The stage samples now use it.
- **Fixed: the LSP did not recognize a staged fragment's header.** For
  `partial workflow W stage S {`, `self.` completion and the other in-step features found
  no enclosing workflow.
- **`///` documents an input.** The `///` lines right above an `input` (a workflow's or an
  agent's) are its documentation; a leading `name:` repeating the input's name is dropped.
  - **Where it shows:** the argument's `"description"` in the MCP/A2A `inputSchema`, which
    an MCP client shows next to the field. It is also in `--dry-run` (`inputs[].doc`),
    `mhl graph` (`inputs[].doc`), the `mhl://workflow` manifest, and the editor's hover
    (with the stage it is asked for in) and completion.
  - **What counts:** a plain `//` stays a private note.
  - **No effect on runs:** the doc is outside `DefinitionDigest`, so editing it never
    invalidates a checkpoint.
  - **In Go:** `ast.PipelineInput.Doc` / `runtime.PipelineInputSpec.Doc`, read by
    `parser.attachInputDocs`.
- **`tools/list` says where a workflow may pause.** Over HTTP, each per-workflow entry of a
  workflow that can suspend for a human carries `_meta["mhl.run"].pauses`:
  `{"steps": [...], "stages": [...]}`. These are the steps whose body calls `pause(...)`
  (static, so a conditional pause counts) and the stages whose inputs have no default. A
  client can then start such a workflow with `run/start` rather than a `tools/call`, which
  can't resume. The key is absent for one that never pauses. In Go:
  `runtime.Pipeline.PauseSteps` / `CanPause()`, from `ast.CallsPause`.
- An enum input's `= Enum.Variant` default now appears in `inputSchema` as `"default"` (the
  bare variant name). Before, only a literal default did.
- `mhl lint` reports an input declared twice across a workflow's fragments, a stage input in
  a `loop` workflow, and a stage input in a stage with no step to enter.

### Changed

- **An `input` in a staged fragment is no longer required when the run starts.** The run asks
  for it on entering the stage instead. A workflow that declared one there and always passed
  it up front behaves as before. A run that left it out used to be refused at admission; it
  now pauses at the stage. Move the input to the entry fragment to keep the old contract.
- **Running a fragment of a partial workflow on its own explains itself.** `mhl run` on a
  fragment file still fails with "expected exactly one `entry step` … found 0". The error now
  adds that you should run the file whose fragment declares the `entry`, since it imports the
  others.

## 1.14.0 — 2026-11-21

### Added

- **`.mhl` is an alias of the `.mh` source extension.** A directory scan picks up both —
  `mhl lint <dir>`, `mhl test <dir>`, `mhl serve mcp|a2a <dir>`, `mhl pack` — as do the
  LSP's cross-file lookups (definition, references, symbols, `goto` and `self.`
  completion); `extension.mhl` / `declarations.mhl` are accepted beside the `.mh` names
  (`extension.mh` still wins when both exist). `.mh` stays canonical: `mhl init` and every
  scaffold write it. An explicit path (`mhl run x.mhl`, `import "x.mhl"`) already worked.

## 1.13.0 — 2026-11-20

### Added

- **`mhl graph --merged` shows a `partial` workflow whole.** Every fragment in the file's
  import closure (or in `--root <entry.mh>`'s) is joined in run order: one start edge to the
  `entry` step, and a fragment's last step falling through into the next fragment's first.
  A node, input, var, prop or route from another fragment carries that file's absolute path
  in `"file"`, its spans indexing that file.

- **`stage` — named phases.** A `partial` fragment names its phase on its header —
  `partial workflow Story stage Implementation { ... }` — putting every step and parallel
  group of the file in it, with no block and no extra indentation: a workflow that wants
  stages is split by phase, one fragment per stage. The steps run exactly as without it —
  same step names, same checkpoints — and `goto Name` enters the stage at its first member,
  from any fragment. The stage reaches observers: `StepContext.stage` (optional field; `null`
  outside a stage) in `step_start`/`step_end`, `stage` on `--format jsonl` step events (and the
  stage list on `run_start`), `run/status`, the `mhl://workflow/<name>` manifest, the
  `--dry-run` plan (`named_stages` in its JSON) and `mhl graph` (`stages`, `Node.stage`).
  `mhl lint` rejects a stage on a declaration that is not `partial`, a stage named like a
  step, group or route, and fragments sharing a stage that are not imported consecutively.
  Programs without a stage keep their `DefinitionDigest`.
- **`mhl run --resume --from <stage>` redoes a phase.** A stopped run (paused, failed or
  cancelled) re-enters the named stage at its first step instead of the step it stopped at,
  with the variables as of the stop — nothing rolled back — and any gate it waited at dropped.
  Also `run/resume {runId, from}` and the `mhl_run_resume` tool's `from`. Refused before
  anything runs (`invalid_from` / `-32602`) for a name that is not a stage, with a decision,
  or without a stopped run.
- **Lint warnings.** A finding may now be a `warning:` — advice that `mhl lint` and
  `mhl run --dry-run` list without failing (the language server shows it as a warning,
  `mhl graph` as `"severity": "warning"`). The first one: a `goto` from outside a stage to
  one of its steps other than the first — prefer `goto <stage>`, so other fragments depend
  on the phase's name and the stage decides where it begins.
- **Typed gates: `pause(reason, decision: T)`.** A pause that returns the decision a resume
  passes. The run reports `awaiting` — the step and `T` (a declared `type`/`enum`, or
  `string`/`number`/`bool`) as JSON Schema — in `mhl run --format json|jsonl`, `run/status`
  and a paused `tools/call`. `mhl run --resume --decision '<json>'` and
  `run/resume {runId, decision}` answer it: checked against `T` before anything runs (a
  wrong one is refused with the run still paused: `invalid_decision` / `-32602`), then
  returned by that gate — once, so the next gate reached pauses again and an old approval
  never answers a later round. In a test, `Name.run(decisions: [...])` answers each gate in
  turn. `mhl lint` checks `T`, and rejects a gate in a `loop` workflow (not supported yet).
  A plain `pause(reason)` is unchanged. `mhl graph` lists a step's gate types in
  `decisions`. A step may hold several gates: re-entered on resume, the gates it already
  passed return their answers again (`Checkpoint.Gate.answered`) instead of pausing.

### Fixed

- **`make functional-test` lints `sample/` too**, which caught a sample whose fragment fell
  through into another file.
- **`mhl graph` no longer draws a start edge into a `partial` fragment without the `entry`
  step.** A run never starts there; the edge pointed at whichever step came first.

## 1.12.0 — 2026-11-20

### Added

- **`cmd.exec(..., cwd: dir)` — run a command in another directory.** `cmd.exec` and
  `cmd.exec_all` take an optional named `cwd:`, the directory the process starts in, as
  `cd dir && command` would: a relative command path (`./gradlew`) resolves there, and a
  relative `cwd` against mhl's working directory. A missing or non-directory `cwd` (or a
  non-string one) raises before any process starts; `""` or `null` inherits mhl's working
  directory. Previously `cwd:` was silently ignored, so a harness running a project's tests
  in another checkout needed a wrapper (`sh -c "cd ..."` or a Python `subprocess` adapter).

### Repository

- **`release.sh --bump vX.Y.Z`** turns `## Unreleased` into `## X.Y.Z — <date>` under a new,
  empty `## Unreleased`, and `release.sh vX.Y.Z` refuses to tag a version without its
  section. 1.11.0 and 1.11.1 had shipped with their notes still under `Unreleased`; they now
  have their own sections.

## 1.11.1 — 2026-11-17

No changes to the language or the `mhl` CLI: the runtime is built from the same source as
1.11.0. Only the VS Code extension shipped with the release changed (0.7.2, see
[its changelog](src/vscode-mhl/CHANGELOG.md)).

## 1.11.0 — 2026-11-15

### Added

- **`entry route` — a workflow can start with a dispatch.**
  `entry route Choose(model: AgentModel) { ... }` is evaluated before any step, its
  parameter bound to the workflow input of the same name, and the run continues at the
  step its arm selects. It runs as a step named after the route (so it appears in
  `executed`, step events and checkpoints) and stays callable with `goto Choose(x)`.
  `mhl lint` allows one per workflow and checks the input's name and type. In a
  `partial` workflow it counts as the single entry.
- **`merge` — an explicit join for a `parallel` group.** A `merge (branches) { ... }`
  block, last in the group, runs once after every branch (as the step `<Group>.merge`)
  with `branches.<Step>.<var>` holding what each branch left. Branches may then write the
  same var; the merge decides the result. `merge { ... }` takes no parameter.
- **Group vars.** A `parallel` group may declare its own `var`s before its branches:
  fresh each time the group starts, shared by its branches and its merge as `self.x`,
  gone after it.
- **`xs.append(x)` as a statement** means `xs = xs.append(x)`; as a value, `.append`
  still leaves its receiver alone. `mhl lint` reports other discarded results, such as a
  bare `name.trim()`.
- **`mhl graph <file> [--stdin]`** prints a workflow's control-flow graph as JSON —
  steps, groups, routes, edges, the calls each step makes, and source spans — for
  editors and visual tools.
- **`mhl run --format jsonl`** streams a run as events (`run_start`, `step_start` /
  `step_end`, `log`, `result`), and **`--workflow <name>`** picks which declared
  pipeline/workflow to run.

### Security

- Built with **Go 1.26.11**, which fixes GO-2026-6859 in the standard library: an
  HTTP/1 server crash (a `MaxBytesReader` header race in `net/http`) that
  `mhl serve mcp --http` and `mhl serve a2a` were exposed to.

### Fixed

- **A cancelled agent or `cmd.exec` no longer lingers.** When the cancel (from
  `wait any`, a quorum, a timeout or SIGINT) landed while the command's shell was
  starting a child, that child could escape the kill and hold the run until it finished
  on its own (`wait any` waited out the full `sleep 30` of the slower agent). The
  process group is now killed again until the command has really exited.

### Changed

- **SIGINT / SIGTERM cancel a run cleanly.** `mhl run` stops at the next step boundary
  (aborting a blocking call in flight), saves the resume checkpoint, and reports
  `"kind": "cancelled"`; a second signal kills it.

### Repository

- **mhl studio** (`src/mhl-studio`), an optional desktop IDE that draws a workflow as a
  diagram kept in sync with its code, is released separately on `studio-vX.Y.Z` tags
  (see its own `CHANGELOG.md`).
- The VS Code extension moved to `src/vscode-mhl`, and the language's `release.sh` to
  `src/mhl-runtime`.

## 1.10.1 — 2026-11-10

### Fixed

- **Findings in imported modules name the module.** `mhl lint` reports each finding
  with the file and line it is in, not the importing file's. The language server shows a
  finding from another file on line 1, prefixed with `file:line:`.
- **A spread of a literal that is not an array** (`[...1]`) is a lint error.

## 1.10.0 — 2026-11-10

### Changed

- **Every `Agent.run(...)` / `Router.delegate(...)` call is checked,** wherever it is —
  tool bodies, tests, hooks, nested calls — for unknown arguments and wrongly typed
  input values.
- **An agent with `input`s reads only its own scope.** `mhl lint` reports an undefined
  name in such an agent's expressions; an agent without inputs still reads the caller's
  scope.

## 1.9.0 — 2026-11-10

### Added

- **Agent inputs.** `input repo: string [= default]` declares a per-call input that
  `.run(prompt: ..., repo: ...)` fills and the agent's own `cwd:`, `args:`, `stdin:`,
  `log:` and hooks read; a fallback receives the same inputs. `"${repo}"` in a literal
  `args` element or `stdin` is substituted.
- **Array spread** `[a, ...xs]` in array literals, including `args:`. A spread of
  `null` is an error, never an empty array.

## 1.8.0 — 2026-11-10

### Added

- **Agent `cwd:`** — the process's working directory, evaluated per call; a missing
  directory fails before the process starts.
- **`log:` as an expression and `log_mode: "append" | "truncate"`** — a per-call log
  file, emptied at each attempt with `truncate`.
- A non-zero exit now reports the tail of the process's output (`partial stdout:`).

### Changed

- **An unknown agent property or `run` argument is an error** (lint and run), no
  longer silently ignored.

## 1.7.1 — 2026-11-10

No language changes (CI: macOS 15 runners).

## 1.7.0 — 2026-11-10

### Added

- **Agent `timeout:` and `kill_grace:`.** `timeout:` bounds each attempt of a `cli/*`
  or `ollama/*` agent: on expiry its whole process group gets SIGTERM, `kill_grace:`
  (default 10s) to exit, then SIGKILL, and the call fails with a `timeout` error that
  `catch`, `finally`, `retry_on: ["timeout"]` and `fallback` all see. `.run(timeout:)`
  overrides it for one call.
- Agent skills (`skills/`) that teach models to write `.mh`.

### Fixed

- On Windows, a timed-out agent or `cmd.exec` ends its whole process tree (a Job
  Object), no longer orphaning grandchildren.

## 1.6.0 — 2026-11-03

### Breaking

- **`mhl serve mcp` no longer serves a workflow's `.mh` source** as a resource
  (`mhl://workflow/<name>/source`): a client only needs the interface. Reading it now
  returns `-32602`.

### Added

- **Encrypted workflow packs.** `mhl pack <dir> -o <file> [--new-key]` seals a workflow
  directory into a DMPK v2 pack; `mhl serve mcp --http --workflows-pack <file>` serves it
  decrypted in memory only, with the key in `MHL_SERVE_WORKFLOWS_KEY` (removed from the
  environment before anything runs).

## 1.5.0-alpha.5 — 2026-10-11

### Added

- **`extension <kind> <Name> from "<id>"`** names which installed extension serves a
  declaration, so several of one kind can be used side by side.

### Changed

- `extension store` is now **`extension state`**; the old kind name still works.

## 1.5.0-alpha.4 — 2026-10-09

### Changed

- **Faster `mhl serve` startup** on large directories: imported modules are resolved once
  and shared, and files are parsed concurrently.
- `make redhat` (linux amd64 + arm64) and `make mac-apple-silicon` build targets;
  `make release` now also builds linux/arm64. Go 1.26.9, `golang.org/x/net` v0.60.0.

## 1.5.0-alpha.3 — 2026-10-08

### Added

- **Text encodings at the boundary.** `fs.read`, `fs.write`, `fs.append`, `cmd.exec`
  and `cmd.exec_all` take `encoding:` (e.g. `"windows-1252"`, `"utf-16"`,
  `"utf-8-bom"`, `"auto"`); strings stay UTF-8 inside mhl.

## 1.5.0-alpha.2 — 2026-10-02

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

### Added

- `THIRD_PARTY_NOTICES.md` with the licenses of the Go modules linked into `mhl`.

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
