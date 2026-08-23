# Repository Guidelines

## Project Structure & Module Organization

The main implementation is the Go module in `src/mhl-runtime` (`github.com/yanjustino/mhl-runtime`). The CLI entry point is `cmd/mhl/main.go`; packages live under `internal/`, including `parser`, `ast`, `runtime`, `mcp`, `auth`, `skills`, `traffic`, and `tools`. Go tests sit beside the code they cover. End-to-end tests and `.mhl` examples are in `test/e2e` and `test/fixtures`. `dist/` contains generated binaries, `vscode-mhl/` contains editor support, and `specs/` contains documentation.

## Build, Test, and Development Commands

Run commands for the runtime from `src/mhl-runtime`:

```sh
make build                 # Build dist/mhl
make test                  # Run all Go tests
./verify-feature.sh <id>   # Build, vet, and test the runtime
make release               # Cross-compile Linux, macOS, and Windows binaries
make verify-release        # Build release targets and verify they are non-empty
```

For focused iteration, use `go test ./internal/parser` or `go test -run TestName ./...`. Run `gofmt` on changed Go files before submitting.

## Coding Style & Naming Conventions

Use idiomatic Go formatted with `gofmt`, tabs for indentation, documented exported identifiers, and small package-focused changes. Name Go files and packages in lowercase; use `snake_case` only where existing filenames require it. Tests use `_test.go`; prefer table-driven cases for parser and runtime behavior. Keep `.mhl` fixture names descriptive and versioned like `test/fixtures/3.*`.

## Testing Guidelines

The project uses Go’s standard `testing` package. Add tests alongside implementation changes and update fixtures when syntax or semantics change. Run `go test ./...` and `go vet ./...`; use `./verify-feature.sh` for the complete verification path. No coverage threshold is configured, but new behavior should include regression coverage, especially for parsing, credentials, subprocesses, MCP, retries, and state redaction.

## Commit & Pull Request Guidelines

Follow the existing Conventional Commit pattern, for example `feat(development): add ...`, `fix(runtime): ...`, or `test(parser): ...`. Keep commits focused. Pull requests should summarize the change, explain compatibility impacts, link the relevant spec/issue when applicable, and report verification commands. Include screenshots when changing the VS Code extension or other user-facing behavior.

## Security & Configuration Tips

Do not commit API keys, tokens, resolved credentials, or generated state. Use environment-backed references such as `env("KEY")` in examples and verify that logs and checkpoints remain redacted. Review changes affecting subprocess isolation, authentication, MCP access, and caching with the zero-trust requirements in `specs/active/`.
