package cli

import (
	"fmt"
	"io"
	"strings"
)

// logo is the mhl wordmark rendered in ASCII, shown at the top of `mhl help`
// (and its `-h` / `--help` aliases). Kept deliberately small so it fits an
// 80-column terminal without wrapping.
const logo = `
   ███╗   ███╗ ██╗  ██╗ ██╗
   ████╗ ████║ ██║  ██║ ██║
   ██╔████╔██║ ███████║ ██║
   ██║╚██╔╝██║ ██╔══██║ ██║
   ██║ ╚═╝ ██║ ██║  ██║ ███████╗
   ╚═╝     ╚═╝ ╚═╝  ╚═╝ ╚══════╝
   Meta-Harness Language · declarative AI agent pipelines
`

// helpText is the full `mhl help` body: the ASCII logo, a one-line usage
// summary, and every subcommand with a short description and its own usage.
// It is intentionally hand-maintained rather than generated — the CLI has a
// small, stable command surface and the wording here is part of the docs.
func helpText() string {
	return logo + `
usage: mhl <command> [arguments]

Commands:
  init [dir]                 Scaffold an immediately-runnable main.mh (never overwrites)
  run <file.mh> [flags]      Execute a pipeline or workflow
  test <file.mh|dir>         Run every test { ... } block in a file or directory
  lint [dir]                 Statically check .mh files (default ".")
  lsp                        Start the language server over stdio (used by vscode-mhl)
  serve <mcp|a2a> [flags]    Expose workflows as MCP tools or A2A skills
  extension <subcommand>     Manage extensions: list, doctor, init, test, package, install
  version                    Print the mhl version
  help                       Show this help (aliases: -h, --help)

Flags for "run":
  --input key=value          Bind a pipeline input (repeatable)
  --resume [--force]         Resume from the last checkpoint; --force ignores a shape change
  --session <id>             Reuse a named session for durable state
  --format text|json         Output format (default "text")
  --dry-run                  Validate and print the static plan without executing

Examples:
  mhl init myproject
  mhl run main.mh --input name=World
  mhl run pipeline.mh --resume --format json
  mhl test ./sample/features
  mhl serve mcp --http --addr :8080 ./workflows

Language reference: https://mh-language.github.io/mhl-core-runtime/reference.html
`
}

// printHelp writes helpText to out.
func printHelp(out io.Writer) error {
	fmt.Fprint(out, helpText())
	return nil
}

// wantsHelp reports whether args carries a bare `-h` / `--help` flag, so a
// subcommand can print its focused usage instead of running.
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

// helpPath collapses the leading non-flag tokens of args into a command path
// ("serve", "serve mcp", "extension install"), capped at two words — the key
// commandHelp is looked up by.
func helpPath(args []string) string {
	var parts []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			break
		}
		parts = append(parts, a)
		if len(parts) == 2 {
			break
		}
	}
	return strings.Join(parts, " ")
}

// commandHelp holds the focused `mhl <command> --help` body for each
// subcommand, keyed by the command path helpPath produces. Hand-maintained
// alongside the argument parsing in cli.go / serve.go / extension.go.
var commandHelp = map[string]string{
	"init": `mhl init [dir]

Scaffold an immediately-runnable main.mh in dir (default "."). Never
overwrites an existing file.

  mhl init            # scaffold ./main.mh
  mhl init myproject  # scaffold ./myproject/main.mh
`,
	"run": `mhl run <file.mh> [flags]

Execute a pipeline or workflow.

Flags:
  --input key=value     Bind a pipeline input (repeatable)
  --resume              Resume from the last saved checkpoint
  --force               With --resume, proceed even if the pipeline shape changed
  --session <id>        Reuse a named session for durable state
  --format text|json    Output format (default "text"); json emits one typed object
  --dry-run             Validate (parse, imports, lint, input contract) and print
                        the static plan without executing any step

  mhl run main.mh --input name=World
  mhl run pipe.mh --resume --force
  mhl run pipe.mh --dry-run --format json
`,
	"test": `mhl test <file.mh|dir>

Run every test { ... } block. Given a directory, recurse into every .mh
file under it and aggregate one report. Exit non-zero if any assertion
failed or no test blocks were found.

  mhl test main.mh
  mhl test ./sample/features
`,
	"lint": `mhl lint [dir]

Statically scan dir (default ".") for .mh files and report every problem
that would otherwise only surface at run time — broken imports, undeclared
or misconfigured agents, syntax errors — each with a file and line.

  mhl lint
  mhl lint ./workflows
`,
	"lsp": `mhl lsp

Start the Language Server Protocol server on stdin/stdout, for editor
integrations (used by vscode-mhl). Takes no arguments; stdout is a raw
LSP-framed JSON-RPC stream.
`,
	"serve": `mhl serve <mcp|a2a> [flags] [dir]

Expose every pipeline/workflow under dir (default ".") to another agent.

  mcp   as MCP tools — stdio JSON-RPC, or Streamable HTTP with --http
  a2a   as Agent2Agent skills over HTTP JSON-RPC

Run "mhl serve mcp --help" or "mhl serve a2a --help" for the flag list.
`,
	"serve mcp": `mhl serve mcp [dir]
mhl serve mcp --http [flags] [dir]

Expose workflows as MCP tools. Without --http, speaks newline-delimited
JSON-RPC on stdin/stdout (the form an MCP client spawns). Diagnostics go
to stderr.

HTTP flags (env var in parentheses):
  --http                        Serve over Streamable HTTP (POST /mcp)
  --addr host:port              Listen address (default 127.0.0.1:8711)
  --token t                     Bearer token (MHL_SERVE_TOKEN)
  --principal-header h          Trust this header for the caller principal;
                                requires --token (MHL_SERVE_PRINCIPAL_HEADER)
  --state-dir path              Persist session/run state here (MHL_SERVE_STATE_DIR)
  --single-replica              Allow a non-cas extension store (MHL_SERVE_SINGLE_REPLICA)
  --drain-timeout d             Graceful-shutdown drain window (MHL_SERVE_DRAIN_TIMEOUT)
  --max-concurrent-runs n       Cap in-flight async runs (MHL_SERVE_MAX_CONCURRENT_RUNS)

  mhl serve mcp ./workflows
  mhl serve mcp --http --addr :8080 --token $TOK ./workflows
`,
	"serve a2a": `mhl serve a2a [flags] [dir]

Expose workflows as Agent2Agent skills over HTTP JSON-RPC.

Flags (env var in parentheses):
  --addr host:port        Listen address (default 127.0.0.1:8710)
  --token t               Bearer token (MHL_SERVE_TOKEN)
  --principal-header h     Trust this header for the caller principal;
                          requires --token (MHL_SERVE_PRINCIPAL_HEADER)

  mhl serve a2a --addr :8081 --token $TOK ./workflows
`,
	"extension": `mhl extension <subcommand> [args]

Manage extensions vendored under .mhl/extensions/.

Subcommands:
  list             Every entry in .mhl/extensions.lock and its status
  doctor           Validate each; non-zero exit if any is broken
  init <dir>       Scaffold an extension project (manifest + sidecar + README)
  test <dir>       Spawn the extension and smoke-test the protocol
  package <dir>    Refresh the declarations sidecar from the running extension
  install <src>    Vendor and pin an extension; src is a local dir, a git remote
                   (<url>[//<subdir>][#<ref>]), or an archive URL
                   (….tar.gz / .zip[#sha256=<hex>])
`,
}

// printCommandHelp writes the focused help for the command path, falling
// back to the first word's help and then the top-level help.
func printCommandHelp(out io.Writer, path string) error {
	if txt, ok := commandHelp[path]; ok {
		fmt.Fprint(out, txt)
		return nil
	}
	if first, _, cut := strings.Cut(path, " "); cut {
		if txt, ok := commandHelp[first]; ok {
			fmt.Fprint(out, txt)
			return nil
		}
	}
	return printHelp(out)
}
