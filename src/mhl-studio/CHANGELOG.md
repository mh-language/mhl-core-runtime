# Changelog

mhl studio is versioned and released on its own `studio-vX.Y.Z` tags, independent of
the language (`vX.Y.Z`). `release.sh` refuses to tag a version without a `## X.Y.Z`
section here, and the release workflow uses that section as the release notes.

## Unreleased

### Added

- **A split workflow is drawn whole.** A `partial` workflow is graphed with
  `mhl graph --merged`, from the workspace file that declares its `entry` step, so
  every fragment shows in one diagram. Steps from another file are dashed, carry
  that file's name, and are read-only here: double-click one (or its file chip) to
  open its file; the inspector shows it with **Open file** / **Show in code**.
  Renaming or deleting a step that another fragment's `goto` targets is refused,
  and a name another fragment declares counts as taken.

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
