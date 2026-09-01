# Contributing to MHL

Thank you for helping improve the Meta-Harness Language (MHL). Contributions of all sizes are
welcome, including bug reports, feature proposals, documentation improvements, examples, tests,
editor support, and runtime changes.

## Before you start

- Search the existing issues and pull requests before opening a new one.
- For bugs, include a minimal `.mh` example, the behavior you observed, the behavior you expected,
  your operating system, and the output of `mhl version` when available.
- For new language features or changes to existing syntax, open an issue before starting a large
  implementation. Describe the use case, proposed syntax and semantics, alternatives, and any
  compatibility concerns.
- Keep each contribution focused on one problem. Small, self-contained pull requests are easier to
  review and merge.

## Repository layout

- `src/mhl-runtime/` contains the Go implementation of the CLI, parser, interpreter, runtime, LSP,
  and built-in features.
- `src/mhl-extensions/` contains the official external extensions (`mhl-store-s3`,
  `mhl-store-postgres`, `mhl-sql-postgres`, `mhl-cache-redis`), each its own Go module and installed
  with `mhl extension install`. Per module: `make build`/`make test` for the host; `make dist`
  is the metadata-only tree (manifest + README); `make release` adds a binary per platform
  (`bin/<name>-<goos>-<goarch>`, `CGO_ENABLED=0`) and tars it. `make -C src/mhl-extensions release`
  does all four plus a `SHA256SUMS`.
- `sample/` contains executable `.mh` examples under `syntax/` and `features/`. These double as the
  documentation-facing functional test suite (`make functional-test`).
- `tests/` contains scenario suites that are not `go test`: `tests/cloud/` exercises
  `mhl serve mcp` across a pod fleet (and `tests/cloud/k8s/` under a real cluster), and
  `tests/extensions/` exercises external extensions plus the `store-fs` / `store-probe` reference
  adapters. Each subdirectory has a `run-all.sh` regression runner.
- `vscode-mhl/` contains the VS Code extension.
- `docs/` contains the public documentation: the language reference and guides under `docs/site/`,
  plus the standalone spec and design notes (`docs/mhl-language-spec.html`, `docs/mhl-eks-plan.html`).

The runtime is divided into four main areas under `src/mhl-runtime/internal/`:

- `lang/` defines the grammar, AST, parser, and static analysis.
- `engine/` evaluates programs and manages pipeline execution.
- `features/` implements capabilities such as agents, memory, MCP, native operations, and traffic
  handling.
- `cli/` handles command-line arguments and dispatches commands to the other packages.

Dependencies should continue to flow from `cli` to `engine`, and from `engine` to `lang` and
`features`. Feature packages must not depend on the interpreter or CLI.

## Development setup

The runtime requires Go 1.25 or later. Clone your fork, then build and test it from the runtime
directory:

```sh
git clone https://github.com/<your-user>/mhl-core-runtime.git
cd mhl-core-runtime/src/mhl-runtime
make build
make test
```

`make build` creates the runtime binary at `src/mhl-runtime/dist/mhl` and copies a binary to
`sample/mhl` for functional tests.

To work on the VS Code extension, install a current Node.js release and run:

```sh
cd vscode-mhl
npm install
npm run compile
```

Open `vscode-mhl/` in VS Code and press `F5` to launch an Extension Development Host. The extension
uses `mhl lsp`, so build the runtime first and ensure that the `mhl` binary is on your `PATH`, or set
`mhl.serverPath` in VS Code.

## Making changes

Create a branch with a short, descriptive name:

```sh
git switch -c feat/datetime-support
```

Follow these guidelines while implementing your change:

- Use idiomatic Go and run `gofmt` on every changed Go file.
- Keep packages focused and preserve the dependency boundaries described above.
- Add tests beside the code they cover. Table-driven tests are preferred when several parser or
  runtime cases share the same structure.
- Add regression coverage for bug fixes.
- Use descriptive, versioned fixture names when adding files under `src/mhl-runtime/test/fixtures/`.
- Update executable examples when syntax or behavior changes.
- Do not commit generated binaries, credentials, tokens, resolved secrets, local state, or editor
  settings.
- Use environment-backed credential references such as `env("KEY")` in examples. Logs,
  diagnostics, and checkpoints must not expose resolved credentials.

### Language change checklist

A language change may affect more than the parser. Check each of the following areas and update all
that apply:

- grammar, lexer, and AST types in `internal/lang/`;
- evaluation and runtime behavior in `internal/engine/`;
- feature implementation in `internal/features/`;
- lint diagnostics and LSP completion or symbols;
- focused unit and regression tests;
- executable examples under `sample/`;
- the language reference under `docs/site/`;
- VS Code syntax highlighting, when new syntax or keywords are introduced.

Clearly document error behavior, serialization rules, and backward-compatibility implications for
new syntax or runtime values.

## Testing and verification

Run focused tests while developing. For example:

```sh
cd src/mhl-runtime
go test ./internal/lang/parser
go test -run TestName ./...
```

Before opening a pull request, run the same runtime checks used by CI:

```sh
cd src/mhl-runtime
go vet ./...
make build
make test
make functional-test
```

CI (`.github/workflows/ci.yml`) runs exactly those four, in that order, and only on changes under
`src/mhl-runtime/`. Changes to `src/mhl-extensions/`, the `tests/` scenario suites, or `docs/` are
not CI-gated: run the affected `tests/**/run-all.sh` (and `go test ./...` in a touched extension
module) yourself, and report the result in the pull request.

For VS Code extension changes, also run:

```sh
cd vscode-mhl
npm run compile
```

Manually verify user-facing editor changes in an Extension Development Host and include screenshots
in the pull request when the visual behavior changes.

## Commit messages

Use Conventional Commits and keep commits focused. Common examples include:

```text
feat(parser): add datetime literal support
fix(runtime): preserve timezone offsets
test(lint): cover invalid datetime values
docs(reference): document datetime operations
```

Use an imperative, concise subject line. If a change is incompatible, explain the migration path in
the commit body and mark the breaking change according to the Conventional Commits format.

## Pull requests

In your pull request:

- explain the problem and the chosen solution;
- link the relevant issue or specification;
- describe syntax, behavior, and compatibility changes;
- list the verification commands you ran and their results;
- include or update tests and documentation;
- include screenshots for visible VS Code or documentation changes;
- call out follow-up work that is intentionally outside the pull request's scope.

Before requesting review, make sure the branch contains no unrelated changes and that all required
checks pass. Review feedback is part of the collaboration process; keep discussions focused on the
code and update the pull request as needed.

## License

By contributing to this repository, you agree that your contributions will be licensed under the
[MIT License](LICENSE.md).
