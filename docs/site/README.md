# MHL documentation site

This directory contains the public, self-contained en-US documentation for the Meta-Harness Language, ready for static publishing. `index.html` is the orientation page: motivation, positioning, quickstart, mental model, feature map, and an applied example. `reference.html` documents syntax, language constructs, built-in extension kinds, pipelines, tests, and CLI behavior. `stdlib.html` is the callable reference: collection methods, `agent`/`prompt`/`memory`/extension methods, native operations, and test assertions with parameters, types, and return values. `extensions.html` guides runtime developers through external extension manifests, permissions, the JSON-RPC process protocol, and installation.

## Publishing

Configure GitHub Pages to publish `docs/site`, or use a Pages workflow with this directory as its artifact. The site requires no build step or backend.

Syntax highlighting is implemented in `app.js`, so MHL is highlighted even when GitHub does not recognize `mhl` as a native Markdown code-block language.
