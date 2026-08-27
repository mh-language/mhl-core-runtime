# features

Examples of mhl's higher-level building blocks — the declarations a pipeline is made of, and
how they're wired together at runtime.

- [agents/](agents/README.md) — declaring `agent`s and calling `.run()`
- [memory/](memory/README.md) — declaring `memory` stores and reading/writing them
- [prompts/](prompts/README.md) — declaring and rendering `prompt` templates
- [pipelines/](pipelines/README.md) — wiring an agent into a `pipeline` `step`
- [mcp/](mcp/README.md) — declaring an `mcp_server` and calling it with `.call(...)`
- [time/](time/README.md) — datetime as plain RFC3339 strings: `time.now/parse/format/add/diff/compare`
- [git/](git/README.md) — the `git` native namespace: `git.status/diff/log/rev_parse/add/commit`
