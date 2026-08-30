package interpreter

import (
	"fmt"
	"io"
	"net/http"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
)

// sessionExtensions, when set, contributes the external extensions the CLI
// discovered for this invocation (from .mhl/extensions.lock). It is a hook so
// the engine never imports internal/extension/external — the CLI owns the
// process lifecycle and closes them at the end of the run. nil = builtins
// only (tests, the LSP).
var sessionExtensions func() []extension.Extension

// SetSessionExtensions installs the provider. The CLI calls it once at the
// start of `mhl run`/`mhl test` and clears it (nil) at the end.
func SetSessionExtensions(f func() []extension.Extension) { sessionExtensions = f }

// newExtensionRegistry builds the Registry for one mhl execution: a fresh
// in-process host (shared HTTP transport, auth-backed secret resolution,
// redacted logging to out) seeded with every built-in extension plus any
// session (external) extensions.
//
// The built-in set lives in internal/extension (extension.Builtins) so lint
// and the LSP can read its metadata without importing the engine; the MCP and
// A2A adapters register into it via internal/extbuiltin, which the CLI blank
// imports. Bloc A keeps this cheap-and-fresh per top-level evalCtx, mirroring
// aliasTypesFor; the "one Registry per execution" optimisation in the plan's
// performance section is a later change — the session extensions are the same
// *External pointers across every per-context registry, so each still owns
// exactly one process regardless of how many registries reference it.
func newExtensionRegistry(out io.Writer) *extension.Registry {
	host := &inProcessHost{client: http.DefaultClient}
	if out != nil {
		host.log = func(line string) { fmt.Fprintln(out, line) }
	}
	reg := extension.NewRegistry(host)
	for _, ext := range extension.Builtins() {
		reg.Register(ext)
	}
	if sessionExtensions != nil {
		for _, ext := range sessionExtensions() {
			if err := reg.TryRegister(ext); err != nil && out != nil {
				fmt.Fprintf(out, "warning: extension %q not loaded: %v\n", ext.ID(), err)
			}
		}
	}
	return reg
}
