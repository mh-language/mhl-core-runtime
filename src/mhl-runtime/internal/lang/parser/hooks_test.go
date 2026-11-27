package parser

import (
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// `on <event>[(param)] { ... }` parses into the same property-with-a-lambda
// the older `<event>: (param) -> { ... }` spelling does, marked On; without a
// parameter the lambda still takes one, named ast.HookParamOmitted.
func TestOnHookExpandsToProperty(t *testing.T) {
	prog, err := Parse(`
workflow P {
    on step_end(step) { log(step.step) }
    on session_start { log("start") }
    step One { }
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	body := prog.Decls[0].Pipeline.Body
	for i, want := range []struct{ name, param string }{{"step_end", "step"}, {"session_start", ast.HookParamOmitted}} {
		m := body[i]
		if m.Hook != nil || m.Prop == nil || !m.Prop.On || m.Prop.Name != want.name {
			t.Fatalf("member %d: want an On property %q, got %+v", i, want.name, m)
		}
		l, ok := ast.LambdaValue(m.Prop.Value)
		if !ok || len(l.Params) != 1 || l.Params[0].Name != want.param || len(l.Block) != 1 {
			t.Fatalf("member %d: want a one-statement lambda of (%s), got %+v", i, want.param, l)
		}
	}
}
