# features

Examples of mhl's higher-level building blocks — the declarations a pipeline is made of, and
how they're wired together at runtime.

- [agents/](agents/README.md) — declaring `agent`s and calling `.run()`
- [memory/](memory/README.md) — declaring `memory` stores and reading/writing them
- [prompts/](prompts/README.md) — declaring and rendering `prompt` templates
- [pipelines/](pipelines/README.md) — wiring an agent into a `pipeline` `step`
- [mcp/](mcp/README.md) — declaring an `extension mcp` and calling it with `.call(...)`
- [a2a/](a2a/README.md) — declaring an `extension a2a` and calling a remote A2A agent with `.send(...)`/`.agent_card()`/`.get_task(...)`/`.cancel(...)`
- [time/](time/README.md) — datetime as plain RFC3339 strings: `time.now/parse/format/add/diff/compare`
- [uuid/](uuid/README.md) — the `uuid` native namespace: `uuid.v4` (random) and `uuid.v7` (time-ordered)
- [git/](git/README.md) — the `git` native namespace: `git.status/diff/log/rev_parse/add/commit`
- [http/](http/README.md) — the `http` native namespace: one op per verb (`get/post/put/patch/delete/head/options`) plus `download`, with `query`, `body`/`text`/`form`, `auth`, `tls` (PEM client certificates), `proxy`, and secret redaction
