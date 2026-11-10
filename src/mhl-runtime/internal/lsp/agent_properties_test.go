package lsp

import (
	"strings"
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

// An agent's declared inputs join its run signature as named parameters
// after the run arguments — optional ("?") when they carry a default.
func TestSignatureHelpAgentRunListsInputs(t *testing.T) {
	src, pos := posAtMarker(t, `
agent Coder {
    input repo: string
    input dirs: string[] = []
    command: "codex"
    cwd: repo
}

pipeline P {
    step S {
        var out = Coder.run(prompt: "p", §)
    }
}
`)
	sh := signatureHelpAt("main.mh", src, pos)
	if sh == nil {
		t.Fatal("expected signature help inside Coder.run(...)")
	}
	label := sh.Signatures[0].Label
	if want := "timeout?: duration, repo: string, dirs?: string[]) -> string"; !strings.Contains(label, want) {
		t.Errorf("label = %q, want it to contain %q", label, want)
	}
	var names []string
	for _, p := range sh.Signatures[0].Parameters {
		names = append(names, p.Label)
	}
	if got := strings.Join(names, ","); got != "prompt,schema,skills,timeout,repo,dirs" {
		t.Errorf("parameters = %s", got)
	}
}

// An agent without inputs keeps the generic run signature.
func TestSignatureHelpAgentRunWithoutInputs(t *testing.T) {
	src, pos := posAtMarker(t, `
agent Coder {
    command: "codex"
}

pipeline P {
    step S {
        var out = Coder.run(§)
    }
}
`)
	sh := signatureHelpAt("main.mh", src, pos)
	if sh == nil || sh.Signatures[0].Label != agentMethodSigs["run"].Label {
		t.Fatalf("signature = %+v", sh)
	}
}

// `input` is offered inside an agent body, next to its properties.
func TestAgentBodyCompletesInput(t *testing.T) {
	src, pos := posAtMarker(t, `
agent Coder {
    command: "codex"
    §
}
`)
	for _, it := range completionAt("main.mh", src, pos) {
		if it.Label == "input" {
			return
		}
	}
	t.Error("input not offered inside an agent body")
}
