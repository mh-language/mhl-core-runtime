# Changelog

mhl studio is versioned and released on its own `studio-vX.Y.Z` tags, independent of
the language (`vX.Y.Z`). `release.sh` refuses to tag a version without a `## X.Y.Z`
section here, and the release workflow uses that section as the release notes.

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
- **New files from templates** that already lint clean: workflow, tool, agent,
  prompt, memory, MCP server, type, enum and test.
- Installers for macOS (Apple Silicon and Intel), Windows and Linux, each bundling
  the `mhl` runtime it was built with.
