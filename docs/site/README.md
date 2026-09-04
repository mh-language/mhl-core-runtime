# MHL documentation site

Public, self-contained en-US documentation for the Meta-Harness Language, ready for
static publishing. No build step and no backend — every page is a standalone HTML file
with its own stylesheet.

| File | What it is |
| --- | --- |
| `index.html` | First-contact guide: the language shape, the anatomy of a workflow, determinism/audit, MCP and A2A serving, and a 30-second install-and-run. |
| `Docs-Reference.dc.html` | The practical reference with copyable examples: modules, syntax and types, expressions, control flow, `agent`, `prompt`, `memory`/`mem`, `tool` and native operations, `extension mcp`/`extension a2a`, `pipeline`/`workflow`, tests, and the CLI. |
| `Docs-Specification.dc.html` | The ECMA-style specification: lexical and syntactic grammar, values and types, expressions, statements, declarations, standard library, native operations, agents, memory, extensions, pipelines, tests, CLI/serving, security, plus a grammar annex and a complete example. |
| `Docs-Servers.dc.html` | The server side of `mhl serve`: stdio and Streamable HTTP MCP transports, the async `run/*` methods and their `mhl_run_*` control tools, `pause()`/`run/resume` for human-in-the-loop, `mhl://` resources, per-caller ownership, `--state-dir`, and A2A skills. |
| `Docs-Extensions.dc.html` | For runtime developers: external extension manifests, permissions, the newline-delimited JSON-RPC process protocol, and locked installation. |
| `Docs-Proposta.html` | A pt-BR executive deck, not part of the language documentation set. |
| `review-types.mh` | The one-line module `index.html`'s tour snippet imports. |
| `styles.css`, `docs-modern.css`, `specification.css`, `serve.css`, `extensions.css` | Per-page stylesheets. |
| `support.js` | The design-canvas runtime the `.dc.html` pages load; generated, do not hand-edit. |

Page behavior — search, theme toggle, en/pt toggle, copy buttons, and the MHL syntax
highlighter that colors every `<code class="language-mhl">` block — lives in an inline
`<script>` at the bottom of each page, so a page stays self-contained when copied
elsewhere.

## Keeping it honest

Every `.mh` snippet on these pages is meant to be accepted by the runtime in
`src/mhl-runtime`. To re-check after a language change, extract the
`<code class="language-mhl">` blocks and run them through `mhl lint`; standalone
programs must report *No problems found.* (Deliberate fragments — a few statements with
no enclosing pipeline, or a call to an agent declared elsewhere on the page — will
report a parse or "not found" error and are fine.)

## Publishing

Configure GitHub Pages to publish `docs/site`, or use a Pages workflow with this
directory as its artifact.
