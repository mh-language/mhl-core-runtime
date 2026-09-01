# MHL documentation site

This directory contains the public, self-contained en-US documentation for the Meta-Harness Language, ready for static publishing. `index.html` is the first-contact guide: the language shape, core features, installation, first run, external extensions, and MCP/A2A servers. `how-it-works.html` explains the pipeline anatomy, runtime lifecycle, controlled loop, and the role of agents with diagrams. `reference.html` is the practical language reference with copyable examples. `mhl-language-spec.html` is the site-styled copy of the ECMA-style specification in `doc/ecma/`, covering grammar, runtime semantics, standard library, extensions, serving, tests, and security. `serve.html` is the server-side guide: exposing a directory of workflows as MCP tools or A2A skills with `mhl serve` — stdio and Streamable HTTP transports, the async `run/*` methods, `run/resume` for human-in-the-loop, per-session ownership, and `--state-dir` (source of truth: `sample/serve/`). `stdlib.html` is the callable reference: collection methods, `agent`/`prompt`/`memory`/extension methods, native operations, and test assertions with parameters, types, and return values. `extensions.html` guides runtime developers through external extension manifests, permissions, the JSON-RPC process protocol, and installation.

## Publishing

Configure GitHub Pages to publish `docs/site`, or use a Pages workflow with this directory as its artifact. The site requires no build step or backend.

Syntax highlighting is implemented in `app.js`, so MHL is highlighted even when GitHub does not recognize `mhl` as a native Markdown code-block language.
