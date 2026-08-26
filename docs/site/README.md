# MHL documentation site

This directory contains the public, self-contained en-US documentation for the Meta-Harness Language, ready for static publishing. The home page covers motivation, positioning, a quickstart, an applied example, the reference research, and contributions. `reference.html` documents the syntax and every language construct with complete examples.

## Publishing

Configure GitHub Pages to publish `docs/site`, or use a Pages workflow with this directory as its artifact. The site requires no build step or backend.

Syntax highlighting is implemented in `app.js`, so MHL is highlighted even when GitHub does not recognize `mhl` as a native Markdown code-block language.
