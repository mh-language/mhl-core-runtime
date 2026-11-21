# Changelog

mhl studio is versioned and released on its own `studio-vX.Y.Z` tags, independent of
the language (`vX.Y.Z`). `release.sh` refuses to tag a version without a `## X.Y.Z`
section here, and the release workflow uses that section as the release notes.

## 0.6.0

### Changed

- **Stages open on their own diagram.** A workflow with stages is drawn as its
  phases: each stage is one card (its steps listed, with their run status), and
  the gotos between two stages are merged into one edge, their conditions joined.
  Click a card to open the stage: its steps in its box, with only the steps,
  routes and stages they come from or go to drawn around it. A stage declared in
  another fragment opens that file, so its steps can be edited. The bar over the
  diagram leads back to the whole workflow (or press Esc), and **All steps** shows
  every stage expanded, as before.
- **The phases line up.** The path a run takes when no condition holds — each
  step's fallthrough or unconditional `goto` — is drawn first and straight, so the
  stages stand in one column from Start to End and the conditional exits (a
  rejection, a revision) sit below them, instead of between two phases because
  their step was declared earlier. A stage card shows its name on a line of its own,
  its step count and file underneath.
- **Each edge leaves from the side that fits it.** The main line runs straight
  down the middle, from the centre of each step's bottom into the centre of the
  next one's top. A conditional exit off it (a rejection, a revision) leaves the
  step from the side its target is drawn on — the exits alternate left and right
  in the order they are declared, so one keeps its side in every view — and a goto
  back up (`revise_specs`) leaves and enters from the side, climbing beside the
  diagram instead of over the top. A condition that skips a step runs down its
  right side. A step's ↻ ask-again loop moved up by its header, out of the way.
- **An open stage and "All steps" are laid out like the whole workflow.** A stage's
  steps are laid out with the rest, in a band of layers of their own, and its box
  is drawn around them afterwards, instead of being laid out apart inside it:
  what comes before and after the stage lines up with its steps, and with every
  stage expanded the phases stand in one column, the exits below them.

## 0.5.0

### Added

- **A split workflow is drawn whole.** A `partial` workflow is graphed with
  `mhl graph --merged`, from the workspace file that declares its `entry` step, so
  every fragment shows in one diagram. Steps from another file are dashed, carry
  that file's name, and are read-only here: double-click one (or its file chip) to
  open its file; the inspector shows it with **Open file** / **Show in code**.
  Renaming or deleting a step that another fragment's `goto` targets is refused,
  and a name another fragment declares counts as taken.
- **Stages are boxes.** A stage — the `stage Name` on a `partial` fragment's
  header — is drawn as a titled box around that fragment's steps and parallel
  groups (an ELK compound node) in the whole-workflow diagram; a `goto` to the
  stage lands on the box, which takes its steps' status during a run, and a
  transition drawn onto it writes `goto <stage>`.
- **`stage` is highlighted** in the code editor (the shared TextMate grammar of
  the VS Code extension): the keyword and the stage's name, only in a header's
  `stage Name {`.
- **Redo a stage.** When a run has stopped (paused or failed), each stage box
  shows **↻ redo**: it resumes the run at that stage's first step
  (`mhl run --resume --from`), with the variables as of the stop.
- **Lint warnings look like warnings** in the inspector's Problems list (`warning ·`,
  amber) — such as a `goto` into the middle of another stage.
- **Resume a gate with a form.** A run paused at a typed gate
  (`pause(reason, decision: T)`) shows a decision form built from `T` — a select
  per enum, a checkbox per bool, a field per string/number — and **Resume with
  decision** passes it (`mhl run --resume --decision`). A gate step is labelled
  `Gate · T` on the diagram.

### Changed

- **Big diagrams read better.** Steps are laid out in the order a run reaches
  them, so a shared step declared early (a revision step in the entry fragment)
  sits after its callers instead of above the whole diagram, and a stage keeps
  its steps in the order they are written. A step's guard `fail(...)` no longer
  draws an edge to Fail: its ✕ badge lists the conditions on hover (a
  `goto match`/route arm to `fail` still goes to the Fail terminal). A step that
  asks again (`goto` itself) gets a short `↻` loop on its side, its conditions on
  hover, and a long condition label is cut with the full text in its tooltip. On
  the AgileSpec story workflow: 2386 → 775 px wide, 79 → 42 bends.

## 0.4.0

### Fixed

- **macOS: no more "is damaged and can't be opened".** The app bundle is now
  ad-hoc signed as a whole (`bundle.macOS.signingIdentity: "-"`), not just its
  executable by the linker, so Info.plist and resources are sealed. A downloaded
  copy now gets Gatekeeper's "could not verify" prompt, which System Settings →
  Privacy & Security → **Open Anyway** clears without a terminal. Still not
  Developer ID–signed or notarized.

## 0.3.0

### Added

- **Light/dark theme switch.** A toolbar button next to the runtime version cycles
  the theme through System → Light → Dark. System, the default, follows the OS as
  before and still switches live when the OS does. The choice is remembered across
  launches and applies to the whole window: panels, diagram, code editor and the
  native title bar.

## 0.2.0

### Added

- **Native ARM builds.** Windows on ARM gets its own installer
  (`mhl.studio_X.Y.Z_arm64-setup.exe`) instead of the x64 one under emulation. There
  is no ARM64 `.msi`, because WiX does not support it. Linux on ARM gets an
  `.AppImage`, a `.deb` and an `.rpm`. Each bundles the `mhl` runtime built for ARM64.

## 0.1.0

First public preview. The installers are not code-signed yet.

### Added

- **A workflow diagram kept in sync with the code.** The diagram is drawn from the
  `.mh` file (`mhl graph`). Every diagram action becomes a minimal text edit, as one
  undoable step, and comments and formatting outside it are never touched.
- **States bar:** drag Pass, Choice, Parallel, Human approval, Succeed and Fail onto
  the diagram.
- **Routes on the diagram.** A `route` is a node of its own: callers point at it and
  its arms leave it. An `entry route` is where Start leads. A route no step calls is
  drawn apart, with its arms listed inside.
- **Calls in a step,** one box per agent or tool call. Drop a tool method or an agent
  on a step to add the call, or on empty space for a new step.
- **Toolbox** of the folder's tools, agents, prompts, MCP servers, memory and types.
  Using one from another file adds the `import`, and marks it `export` in its own
  file when needed.
- **Tool builder:** rename a tool, edit its methods' signatures and bodies, and
  **Run method** with real arguments.
- **Code editor** with MHL highlighting and the `mhl lsp` language server.
- **Live runs:** an inputs form, each step's status and the path taken on the
  diagram, event history, logs and final variables. Pause, resume and cancel.
- **MCP test bench** (the toolbar's **Editor | MCP** switch): serves the open folder with
  `mhl serve mcp`, over stdio or HTTP, and calls it like an API client would. You pick a
  tool, fill in a form generated from its `inputSchema`, and **Send**. The response comes
  back as highlighted, foldable JSON, with JSON held in strings shown decoded, or as raw
  text or a table. It also covers the workflow manifests, async runs over HTTP (start,
  follow, resume a paused run, cancel) and raw requests. A console logs every JSON-RPC
  message.
- **New files from templates** that already lint clean: workflow, tool, agent,
  prompt, memory, MCP server, type, enum and test.
- Installers for macOS (Apple Silicon and Intel), Windows and Linux, each bundling
  the `mhl` runtime it was built with.
