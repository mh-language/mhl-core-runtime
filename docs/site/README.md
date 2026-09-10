# MHL documentation site

Public, self-contained en-US documentation for the Meta-Harness Language, ready for
static publishing. No build step and no backend — every page is a standalone HTML file
with its own stylesheet.

| File | What it is |
| --- | --- |
| `index.html` | First-contact guide: the language shape, the anatomy of a workflow, determinism/audit, MCP and A2A serving, and a 30-second install-and-run. |
| `playground.html`, `playground-en.html` | Portuguese and English playgrounds: 17 lessons in four sections, editable MHL, per-lesson drafts, explicit completion, challenges, source links, downloads, and CLI commands. |
| `playground-lessons.js` | Complete `.mh` programs and teaching notes, from values to loops, deadlines, concurrency, recovery, and MCP/A2A. |
| `playground-lessons.en.js` | English lesson text, comments, and program messages; preserves the original lesson IDs, filenames, and execution contracts. |
| `playground-core.js`, `playground.js`, `playground.css` | Source tokenizer, closure diagnostics, defensive storage loading, editor interactions, and responsive layout. |
| `playground.test.cjs` | Node regression tests with optional integration against a locally built MHL binary. |
| `Docs-Reference.dc.html` | The practical reference with copyable examples: modules, syntax and types, expressions, control flow, `agent`, `prompt`, `memory`/`mem`, `tool` and native operations, `extension mcp`/`extension a2a`, `pipeline`/`workflow`, tests, and the CLI. |
| `Docs-Specification.dc.html` | The ECMA-style specification: lexical and syntactic grammar, values and types, expressions, statements, declarations, standard library, native operations, agents, memory, extensions, pipelines, tests, CLI/serving, security, plus a grammar annex and a complete example. |
| `Docs-Servers.dc.html` | The server side of `mhl serve`: stdio and Streamable HTTP MCP transports, the async `run/*` methods and their `mhl_run_*` control tools, `pause()`/`run/resume` for human-in-the-loop, `mhl://` resources, per-caller ownership, `--state-dir`, and A2A skills. |
| `Docs-Extensions.dc.html` | For runtime developers: external extension manifests, permissions, the newline-delimited JSON-RPC process protocol, and locked installation. |
| `Docs-Proposta.html` | A pt-BR executive deck, not part of the language documentation set. |
| `review-types.mh` | The one-line module `index.html`'s tour snippet imports. |
| `styles.css`, `docs-modern.css`, `specification.css`, `serve.css`, `extensions.css` | Per-page stylesheets. |
| `support.js` | The design-canvas runtime the `.dc.html` pages load; generated, do not hand-edit. |

Reference-page behavior — search, theme toggle, en/pt toggle, copy buttons, and the MHL syntax
highlighter that colors every `<code class="language-mhl">` block — lives in an inline
`<script>` at the bottom of each page, so a page stays self-contained when copied
elsewhere.

The Portuguese playground loads its scripts in order: lessons, core, then UI.
The English page also loads `playground-lessons.en.js` immediately after the base
lessons. Publish/copy both pages, all scripts, and `playground.css` together. It requires no
package installation, bundler, CDN editor, backend, or browser-side MHL interpreter.

## Playground maintenance

The browser checks only string/comment closure and paired delimiters. It does not
validate names, types, runtime configuration, or interpolated expressions. Reference
results are labeled as belonging to the original example, including after edits;
they are never presented as the result of executing edited code.

Completion is user-controlled. Drafts, current lesson, and completed lesson IDs are
stored separately per language: the existing `mhl-playground-v2` key for Portuguese
and `mhl-playground-v2-en` for English. The language link preserves the current
lesson's URL fragment; it never translates or overwrites a user's draft. The home
page links to the playground matching its selected language. Unavailable storage keeps editing functional for
the current tab. Tab moves focus by default; indentation is opt-in and Escape
restores focus navigation. Each lesson has a stable URL fragment.

Run browser-independent regression checks from the repository root:

```sh
node --test docs/site/playground.test.cjs
```

To validate all original programs in both languages with the real parser/linter and run all local
examples (including iteration ceilings, timeout failure, and pause/resume), build
the runtime and pass its absolute path:

```sh
cd src/mhl-runtime
go build -o /tmp/mhl-playground-check ./cmd/mhl
cd ../..
MHL_BIN=/tmp/mhl-playground-check node --test docs/site/playground.test.cjs
```

Integration checks generate files and runtime state in an isolated temporary
directory and remove them afterward. The MCP/A2A lesson is linted but never
executed by the tests: its URLs are placeholders and require user configuration.

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
