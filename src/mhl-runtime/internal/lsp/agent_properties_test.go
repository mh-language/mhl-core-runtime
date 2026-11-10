package lsp

import (
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// agentPropertyItems is hand-written (it carries prose), so pin it to the
// runtime's own list: a property the interpreter reads must be completable,
// and a completion must name a property the interpreter accepts.
func TestAgentPropertyItemsMatchRuntime(t *testing.T) {
	items := map[string]bool{}
	for _, it := range agentPropertyItems {
		items[it.Label] = true
		if !ast.IsAgentProperty(it.Label) {
			t.Errorf("completion offers %q, which the runtime rejects", it.Label)
		}
	}
	for _, name := range ast.AgentPropertyNames() {
		if !items[name] {
			t.Errorf("runtime reads %q but completion doesn't offer it", name)
		}
	}
}
