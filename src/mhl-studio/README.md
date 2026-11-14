# mhl studio

A desktop IDE for designing and running mhl workflows, in the spirit of the AWS Step Functions
Workflow Studio. You build a workflow by dragging states onto a diagram, and the diagram and
the `.mh` code stay in sync in both directions. Tauri 2 (Rust) + React + React Flow + Monaco.

- **Design:** a bar of flow states above the diagram (Pass, Choice, Parallel, Human approval,
  Succeed, Fail), an auto-laid-out diagram of the workflow, and an inspector for the selected
  step or for the workflow itself.
- **New files:** the explorer's **+** asks what the file declares — workflow, tool, agent,
  prompt, memory, MCP server, type, enum, test or nothing — and starts it from a template
  that parses and lints clean, its declaration named after the file
  (`flows/review_bot.mh` → `ReviewBot`).
- **Toolbox (left sidebar):** a single outline of what a workflow can call or use, grouped by
  kind: Tools (with their methods), Agents, Prompts, MCP & extensions, Memory, Types. It lists
  both this file's declarations and those of the folder's other `.mh` files; the latter show
  their file's name, plus ✓ once imported. Using an item from another file imports it:
  dragging a method or an agent onto the diagram adds `import { Name } from "./file.mh"`
  (merged into an existing import of that file). If the declaration isn't `export`ed yet, it
  is marked `export` in its own file first, since a named import requires it. To import
  without using, hover the item and click ⤓. A filter box narrows the list. New tool, agent,
  prompt, … files start out `export`ed.
- **Calls in a step:** each step draws one box per agent / tool call it makes (from
  `mhl graph`'s `callSites`); click a box to see the call in the code. Dropping a tool method
  or an agent *onto* a step adds the call inside that step, as `var x = Tool.method(…)` or
  `var reply = Agent.run(prompt: …)`. If the step always leaves through its last statement
  (`goto`, `complete()`, …), the call goes just before it. Dropping on empty diagram space
  creates a new step instead.
- **Tools:** build the `tool`s your steps call. **+** in the Tools list adds one. In the
  inspector you can rename a tool, add or delete methods, and edit a method's signature
  (parameters with types and defaults, return type, `internal`) and its body (an
  expression, or a block of statements). **Run method** calls a method with real arguments.
  Dragging a method onto the diagram adds a step that calls it.
- **Code:** a Monaco editor with mhl highlighting (vscode-mhl's TextMate grammar) and the
  `mhl lsp` language server (diagnostics, completion, hover, signature help, go-to-definition).
- **Run:** an inputs form built from the workflow's declared inputs. While a run executes,
  each step's status is drawn live on the diagram and the path taken is highlighted. Below
  the diagram are an event history, logs and the final variables. A run that hits
  `pause(...)` can be approved and resumed, and a run in flight can be cancelled.

## How it works

**The code is the single source of truth.**

- **The diagram is a projection of the code.** The diagram comes from
  `mhl graph <file> --stdin` (`src/mhl-runtime/internal/lang/flow`). That command reports
  every step, parallel group and transition, each with the byte span of the source it came
  from.
- **Diagram edits become text edits.** A diagram or inspector action is turned into minimal
  text edits against those spans (`src/sync/edits.ts`). The edits are applied to the Monaco
  model as one undoable step, and the file is then graphed again.
- **Formatting survives.** Comments and formatting outside the edited range are never
  touched, because the parser drops comments and so the program is never re-printed.
- **Editing the code redraws the diagram.** If the code stops parsing, the diagram keeps the
  last version that did, read-only, until the code is fixed.

**The runtime is vendored.** `npm run sidecar` builds `src/mhl-runtime` for the target into
`src-tauri/binaries/mhl-<target-triple>`, and Tauri bundles it as an external binary. The
studio only ever runs that copy, never an `mhl` on `PATH`, so the language it edits is the
runtime it ships. That copy is used for:

| Feature | `mhl` command |
|---|---|
| Diagram | `mhl graph <file> --stdin` |
| Input form, validation | `mhl run <file> --dry-run --format json [--workflow W]` |
| Execution | `mhl run <file> --format jsonl [--workflow W] [--resume --session S]`: streams `run_start` / `step_start` / `step_end` / `log` / `result` events. Cancel sends SIGINT, which saves a resumable checkpoint. |
| Editor intelligence | `mhl lsp`, bridged over stdio by `src-tauri/src/lsp.rs` |
| Tool "Run method" | `mhl run <tmp>/eval.mh --workflow __mhl_studio_eval --format json`, where `eval.mh` imports the saved file and runs one step, `self.result = Tool.method(…)`, with checkpointing off, so nothing is written to the workspace. The language has no "evaluate an expression" command, so `src-tauri/src/mhl.rs` (`evaluate`) writes that throwaway program. |

| Palette state | Generated `.mh` |
|---|---|
| Pass | `step Step { }` |
| Choice | `step Choose { goto match … { "a" -> A  _ -> fail(…) } }` |
| Parallel | `parallel Fan { step FanLeft { } step FanRight { } }` |
| Human approval | `step Approve { pause("…") }` |
| Succeed / Fail | `complete()` / `fail("…")` |

Tool edits work the same way (`src/sync/tools.ts`), against the spans `mhl graph` reports for
each `tool`: the tool's name, its braces and its `Name.` call sites, and each method's header
and body.

- **Renaming a tool** rewrites its call sites in this file.
- **Renaming a method** rewrites `Tool.method` and `self.method` in this file.
- **Files that import the tool** are not rewritten.

On the diagram:

- **Connecting two steps** appends `goto Target`, or retargets the step's trailing `goto`.
- **Deleting a transition** removes its `goto` or `match` arm line.
- **Deleting a fall-through transition** ends the step with `complete()`.

**Drag and drop is HTML5, so the window sets `"dragDropEnabled": false`.** That option is
in `tauri.conf.json`. With Tauri's default (`true`), the native layer claims every drag over
the window to report OS file drops. The page then never receives `drop`: on macOS, wry skips
WebKit's `performDragOperation`. The studio takes no file drops, so it turns that layer off.

## Develop

Requirements:

- Go (the version in `src/mhl-runtime/go.mod`)
- Node 22
- Rust stable
- On Linux, the Tauri system dependencies (WebKitGTK 4.1, GTK 3, librsvg)

```sh
npm install
npm run tauri dev       # builds the sidecar, starts Vite and the app
npm run tauri build     # a release bundle for this platform (sidecar included)
```

Checks (the same as `.github/workflows/studio.yml`, which is not part of the language's release gate):

```sh
npm run sidecar && npm run typecheck && npm test && npx vite build
(cd src-tauri && cargo clippy --all-targets -- -D warnings && cargo test)
```

`npm test` runs every diagram edit against the real vendored `mhl graph`, so run
`npm run sidecar` first. Each edit test applies the edit, graphs the result again, and checks
that the program still parses and has the expected shape.

## Layout

```
scripts/build-sidecar.mjs   vendor src/mhl-runtime into src-tauri/binaries/
src-tauri/src/              sidecar.rs (locate/spawn mhl), mhl.rs (graph, dry-run, evaluate),
                            run.rs (jsonl streaming, cancel), lsp.rs (LSP framing),
                            workspace.rs (folder, .mh files)
src/sync/                   offsets.ts (UTF-8 bytes ↔ UTF-16), edits.ts (diagram edit →
                            text edit), tools.ts (tool builder edits), imports.ts (import /
                            export edits), templates.ts (palette, new files)
src/canvas/                 ELK layout, React Flow nodes/edges, drag & drop
src/editor/                 Monaco setup (slim: no bundled languages), LSP client
src/inspector/, src/palette/, src/run/, src/app/ (store, controller, shell)
```

## Known limits

- Windows has no graceful cancel. A console-less child cannot receive CTRL_BREAK, so cancel
  terminates the process tree, and a resume re-runs the step that was interrupted.
- Some transitions cannot be removed from the diagram. Only the ones that map to a whole line
  can be: a plain `goto X`, `if (…) goto X`, or a single `goto match` arm. The rest are
  edited in the code view.
- Release packaging is not wired into CI yet. A `tauri-action` matrix on a dedicated
  `studio-v*` tag is a follow-up. The studio does not cross-compile like the Go runtime.
