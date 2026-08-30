// Package extbuiltin registers the runtime's built-in extension adapters
// (MCP, A2A) into internal/extension's built-in set. It exists only for its
// init side effect: import it for side effects — `_ ".../internal/extbuiltin"`
// — from every entry point that needs the built-ins available (the CLI and
// the LSP). The interpreter reads the set through extension.Builtins() and
// does not need this import itself.
//
// Keeping this wiring in one package means adding a new built-in extension is
// a one-line edit here, not a change to the CLI, the LSP, and the linter
// separately.
package extbuiltin

import (
	"github.com/mh-language/mhl-core-runtime/internal/extension"
	"github.com/mh-language/mhl-core-runtime/internal/features/a2a"
	"github.com/mh-language/mhl-core-runtime/internal/features/mcp"
)

func init() {
	extension.RegisterBuiltin(mcp.NewExtension())
	extension.RegisterBuiltin(a2a.NewExtension())
}
