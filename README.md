<p align="center">
  <img src="docs/site/assets/mhl-logo.png" alt="mhl logo" width="160">
</p>

# Meta-Harness Language (mhl)

**mhl** is a declarative language for describing AI agent pipelines: agents, tools, memory, MCP servers, prompts, and the pipelines that wire them together. **[Click here for the full language reference](https://mh-language.github.io/mhl-core-runtime/reference.html)**.

## Install

The install scripts download the `mhl` binary and, if VS Code is installed, the `mhl-language`
extension, both from the latest [GitHub Release](https://github.com/mh-language/mhl-core-runtime/releases).

**macOS / Linux:**

```bash
curl -fsSL https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/install.ps1 | iex
```

This installs `mhl` to `~/.mhl/bin` (`%LOCALAPPDATA%\mhl\bin` on Windows) and adds it to your
PATH. Supported platforms today: `linux-amd64`, `linux-arm64`, `darwin-arm64` (Apple Silicon),
`windows-amd64`, `windows-arm64`. Intel Mac (`darwin-amd64`) has no published binary.

### Manual install

Download a binary and/or the `.vsix` directly from the
[Releases page](https://github.com/mh-language/mhl-core-runtime/releases), or build from source:

```bash
# runtime
(cd src/mhl-runtime && make build)   # outputs dist/mhl
# without make:
(cd src/mhl-runtime && ./build.sh build)

# VS Code extension
cd vscode-mhl && npm install && npx @vscode/vsce package   # outputs mhl-language-<version>.vsix
```

Then install the `.vsix` in VS Code via **Extensions → ⋯ → Install from VSIX...**, or run
`vscode-mhl/install.sh`, which builds and installs it in one step.

## Documentation

> [!NOTE]  
> **Note:** The docs are a work in progress. The language is still evolving, and the docs
> 
The full language reference lives at **[mh-language.github.io/mhl-core-runtime/reference.html](https://mh-language.github.io/mhl-core-runtime/reference.html)**.


## Examples

[`sample/`](sample/README.md) has worked, self-verifying `.mh` examples for every language
feature — run any of them directly with `mhl test <file>`:

- [`sample/syntax/`](sample/syntax/README.md) — the expression and statement language itself
  (arithmetic, arrays, objects, strings, conditionals, loops)
- [`sample/features/`](sample/features/README.md) — the higher-level declarations a pipeline
  is built from (`agent`, `memory`, `prompt`) and how a `pipeline` wires them together

## Repository layout

- [`src/mhl-runtime/`](src/mhl-runtime) — the Go implementation of the `mhl` CLI (parser,
  interpreter, runtime, LSP)
- [`vscode-mhl/`](vscode-mhl) — the VS Code extension (syntax highlighting, diagnostics,
  completion), a thin wrapper around `mhl lsp`
- [`docs/site/`](docs/site) — the canonical language reference, deployed to GitHub Pages
- [`sample/`](sample) — worked `.mh` examples, doubling as the docs-facing test suite
- [`tests_e2e/`](tests_e2e) — scenario suites that aren't `go test`: `mhl serve mcp` across a
  pod fleet (`tests_e2e/cloud/`) and external-extension behavior (`tests_e2e/extensions/`)

## Official extensions

The repository also maintains the official external extensions used by MHL:

| Package | Kind | Backend |
| --- | --- | --- |
| [`mhl-blob-s3`](src/mhl-extensions/mhl-blob-s3/) | `blob` | S3-compatible object storage |
| [`mhl-cache-redis`](src/mhl-extensions/mhl-cache-redis/) | `cache` | Redis with TTL |
| [`mhl-sql-postgres`](src/mhl-extensions/mhl-sql-postgres/) | `sql` | PostgreSQL data queries |
| [`mhl-store-postgres`](src/mhl-extensions/mhl-store-postgres/) | `store` | PostgreSQL |
| [`mhl-store-redis`](src/mhl-extensions/mhl-store-redis/) | `store` | Redis durable state |
| [`mhl-store-sqlite`](src/mhl-extensions/mhl-store-sqlite/) | `store` | Local SQLite |

Each extension is a separate Go module with its own manifest, tests, README, and
multi-platform executable. The complete build and release process is documented
in [`src/mhl-extensions/README.md`](src/mhl-extensions/README.md).

### Install an official extension

Extension bundles are published independently from the runtime using tags such as
`extensions-v0.1.0`. A bundle contains one archive for each official extension,
`SHA256SUMS`, and `release.json`; the latter records package versions, supported
platforms, and the exact runtime commit tested with the bundle.

Copy the hash for the package you want from `SHA256SUMS` and install it with MHL:

```bash
mhl extension install \
  'https://github.com/mh-language/mhl-core-runtime/releases/download/<bundle-tag>/mhl-store-postgres.tar.gz#sha256=<sha256>'
mhl extension doctor
```

The runtime selects the matching host binary and records the source and hash in
`.mhl/extensions.lock`. Runtime releases keep the `vX.Y.Z` tag namespace and
remain the repository's GitHub `latest`; extension releases use `extensions-vX.Y.Z`
and do not replace it.

To build and test the official extensions locally:

```bash
make -C src/mhl-runtime build
make -C src/mhl-extensions vet test release
python3 -m unittest discover -s src/mhl-extensions/scripts -p 'test_*.py'
python3 src/mhl-extensions/scripts/package_release.py prepare \
  --runtime "$PWD/src/mhl-runtime/dist/mhl"
```
