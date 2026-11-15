# Repository Guidelines

## Project Structure & Module Organization

The main implementation is the Go module in `src/mhl-runtime` (`github.com/mh-language/mhl-core-runtime`). The CLI entry point is `cmd/mhl/main.go`; packages live under `internal/` in four groups with a one-way dependency order — `lang/` (parser, AST, lint, flow), `engine/` (interpreter, runtime), `features/` (memory, MCP, native ops, tools, auth, traffic) and `cli/` — plus `lsp/`, `execsvc/`, `mcpserver/`, `a2aserver/`, `extension/` and `pack/`. Go tests sit beside the code they cover. Executable `.mh` examples, which double as the functional test suite, are in `sample/syntax` and `sample/features`; cluster scenarios are in `tests_e2e/cloud`. `src/mhl-runtime/dist/` holds generated binaries. The other products are `src/vscode-mhl/` (VS Code extension), `src/mhl-studio/` (the desktop IDE) and `skills/` (agent skills); the documentation site is `docs/site/`. `CLAUDE.md` has the detailed architecture notes.

## Build, Test, and Development Commands

Run commands for the runtime from `src/mhl-runtime`:

```sh
make build                 # Build dist/mhl (and sample/mhl)
make test                  # Run all Go tests
make functional-test       # Run `mhl test` over sample/syntax and sample/features
go vet ./...
make release               # Cross-compile Linux, macOS, and Windows binaries
make verify-release        # Build release targets and verify they are non-empty
```

CI runs `go vet ./...`, `make build`, `make test`, then `make functional-test`. For focused iteration, use `go test ./internal/lang/parser/...` or `go test -run TestName ./...`. Run `gofmt` on changed Go files before submitting.

## Coding Style & Naming Conventions

Use idiomatic Go formatted with `gofmt`, tabs for indentation, documented exported identifiers, and small package-focused changes. Name Go files and packages in lowercase; use `snake_case` only where existing filenames require it. Tests use `_test.go`; prefer table-driven cases for parser and runtime behavior. Keep `.mh` sample names descriptive of the behavior they prove (`append_returns_a_new_array.mh`), and list each new sample in its folder's `README.md`.

## Testing Guidelines

The project uses Go’s standard `testing` package. Add tests alongside implementation changes and add or update `sample/` programs when syntax or semantics change. Run `go test ./...`, `go vet ./...` and `make functional-test`. No coverage threshold is configured, but new behavior should include regression coverage, especially for parsing, credentials, subprocesses, MCP, retries, and state redaction.

## Commit & Pull Request Guidelines

Follow the existing Conventional Commit pattern, for example `feat(development): add ...`, `fix(runtime): ...`, or `test(parser): ...`. Keep commits focused. Pull requests should summarize the change, explain compatibility impacts, link the relevant spec/issue when applicable, and report verification commands. Include screenshots when changing the VS Code extension or other user-facing behavior.

## Security & Configuration Tips

Do not commit API keys, tokens, resolved credentials, or generated state. Use environment-backed references such as `env("KEY")` in examples and verify that logs and checkpoints remain redacted. Review changes affecting subprocess isolation, authentication, MCP access, and caching with particular care (see `SECURITY.md`).
