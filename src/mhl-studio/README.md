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
- **MCP test bench:** the toolbar's **Editor | MCP** switch swaps the editor for a bench that
  serves the open folder with `mhl serve mcp`, the way Claude Desktop or VS Code would see
  it: one tool per workflow, and a manifest resource for each. It is laid out like an API
  client.
  - **Left:** a filterable tree of what the server serves (tools, run control tools,
    resources, runs), each with a method badge.
  - **Middle:** the selected request: method, address and **Send**, then the arguments,
    as a key/value form generated from the tool's `inputSchema` or as JSON.
  - **Below:** the response, with a status, a time and a size. The body can be shown
    **Pretty** (highlighted, foldable JSON), **Raw** or as a **Table**. A string that
    holds JSON, such as an agent CLI's raw output, is shown decoded, marked
    `JSON string`; clicking the mark shows the original string.
  - **Transport:** stdio, or Streamable HTTP on a free loopback port with a random bearer
    token. The URL, token and a ready-made `curl` command can be copied, to point another
    client at the same server.
  - **Async runs (HTTP only):** `run/start` returns at once, and the run is followed under
    **Runs**: progress, steps reached, vars, `run/logs`. A paused run can be resumed with
    new arguments (`run/resume`) and a running one cancelled.
  - **Raw request:** any method with any params.
  - **Console** (bottom): every JSON-RPC message, with status polls hidden by default, and
    the server's own log.
  - **Saved files only:** the server reads the saved files when it starts. The bench flags
    unsaved files, and offers a restart after a save.

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
| MCP test bench | `mhl serve mcp .` (stdio) or `mhl serve mcp --http --addr 127.0.0.1:<free port> .` with `MHL_SERVE_TOKEN` in its environment, managed by `src-tauri/src/mcp.rs`. HTTP requests go through the backend (`mcp_http`, a minimal loopback HTTP/1.1 client), not the webview's `fetch`: the server refuses a non-loopback `Origin`, and a `tauri://` page always sends one. The JSON-RPC client is `src/mcp/` (`rpc.ts`, `connection.ts`). |
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

When you add or update a dependency, write the lockfile with **npm 10** — the npm CI
runs (Node 22): `npx npm@10 install`. npm 11 leaves out the dependencies of optional
packages that don't install on your machine (e.g. the WebAssembly fallback of Vite's
bundler on macOS), and npm 10's `npm ci` then refuses that lockfile as out of sync.

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

## Releasing

The studio is versioned and released on its own `studio-vX.Y.Z` tags, independent of the
language (`vX.Y.Z`) and the VS Code extension (`extensions-vX.Y.Z`).

```sh
./release.sh --bump 0.2.0   # sets the version in tauri.conf.json, package.json,
                            # package-lock.json and Cargo.toml — then commit and push main
./release.sh --dry-run      # every check, no tag
./release.sh                # tags studio-v0.2.0 and pushes it
```

The tag runs `.github/workflows/studio-release.yml`. It reruns `studio.yml`'s checks, checks
that the four versions match the tag, and opens a draft GitHub Release. It then builds the
installers on one runner per target, because Tauri does not cross-compile between
OSes: macOS aarch64 and x86_64 (`.dmg`), Windows x86_64 (`.msi`, `-setup.exe`), Windows
aarch64 (`-setup.exe` only: WiX/MSI has no ARM64 support; built on the x64 runner), and
Linux x86_64 and aarch64 (`.deb`, `.rpm`, `.AppImage`; aarch64 on `ubuntu-22.04-arm`). Each installer bundles the `mhl` sidecar for its target. The
release is published only after every target uploaded. It is never marked "Latest", which
stays the language's release.

Before a release, `CHANGELOG.md` needs a `## X.Y.Z` section (the workflow publishes it as
the release notes), and `THIRD_PARTY_NOTICES.md` must list today's dependencies: run
`npm run notices` after changing any npm or Rust dependency. `studio.yml` fails while it is
stale. The file covers the frontend's npm packages, the Rust crates for every released
target, and the bundled runtime's Go modules, and `tauri.conf.json` ships it (with
`LICENSE.md`) inside every installer.

Running the workflow by hand (`workflow_dispatch`) builds every installer as workflow
artifacts and publishes nothing — a way to try a build before tagging.

The installers are **not code-signed yet**. On macOS, Gatekeeper may say the app "is damaged":
run `xattr -dr com.apple.quarantine "/Applications/mhl studio.app"` once. On Windows,
SmartScreen asks for **More info → Run anyway**. The release notes repeat this. Signing
(Apple Developer ID with notarization, a Windows certificate) is a later step: it is a matter
of adding the secrets `tauri-action` reads.

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
  can be: a plain `goto X`, `if (…) goto X`, a single `goto match` arm or a `route` arm. The rest are
  edited in the code view.
- Released installers are not code-signed yet (see Releasing), and there is no auto-update:
  a new version is downloaded from the GitHub Release.
