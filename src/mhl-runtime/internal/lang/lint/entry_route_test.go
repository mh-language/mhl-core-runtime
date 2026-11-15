package lint_test

import (
	"strings"
	"testing"
)

const entryRouteHead = `
enum Model { Claude, Codex }
`

// An entry route dispatching on an input of its own name and type lints
// clean, and stays callable through goto.
func TestEntryRouteClean(t *testing.T) {
	fs := lintSource(t, entryRouteHead+`
workflow W {
    input model: Model = Model.Claude
    entry route Choose(model: Model) {
        Model.Claude -> A
        _ -> B
    }
    step A { complete() }
    step B { goto Choose(Model.Claude) }
}
`)
	if len(fs) != 0 {
		t.Fatalf("unexpected findings:\n%s", joined(fs))
	}
}

// A typed signature's param is bound like an input, so it can be dispatched on.
func TestEntryRouteOnSignatureParam(t *testing.T) {
	fs := lintSource(t, `
type Req = { kind: string }
workflow W(req: Req) {
    entry route Pick(req: Req) {
        _ -> A
    }
    step A { complete() }
}
`)
	if len(fs) != 0 {
		t.Fatalf("unexpected findings:\n%s", joined(fs))
	}
}

func TestEntryRouteRules(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"no such input", `
    entry route Choose(m: Model) { _ -> A }
    step A { complete() }`, `entry route "Choose" dispatches on input "m", which workflow "W" doesn't declare`},
		{"type mismatch", `
    input model: string = "x"
    entry route Choose(model: Model) { _ -> A }
    step A { complete() }`, `entry route "Choose" parameter "model" is Model, but input "model" is string`},
		{"two entries", `
    input model: Model = Model.Claude
    entry route Choose(model: Model) { _ -> A }
    entry route Again(model: Model) { _ -> A }
    step A { complete() }`, "workflow \"W\" has more than one `entry route` (\"Choose\" is already its entry)"},
		{"name of a step", `
    input model: Model = Model.Claude
    entry route A(model: Model) { _ -> B }
    step A { complete() }
    step B { complete() }`, `entry route "A" has the name of a step of workflow "W"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := joined(lintSource(t, entryRouteHead+"workflow W {"+c.body+"\n}\n"))
			if !strings.Contains(got, c.want) {
				t.Fatalf("want a finding containing %q, got:\n%s", c.want, got)
			}
		})
	}
}

// `entry route` is a route: a pipeline can't have one.
func TestEntryRouteNeedsAWorkflow(t *testing.T) {
	got := joined(lintSource(t, entryRouteHead+`
pipeline P {
    input model: Model = Model.Claude
    entry route Choose(model: Model) { _ -> A }
    step A { log("a") }
}
`))
	if !strings.Contains(got, "`route` is only valid inside a `workflow`") {
		t.Fatalf("want the workflow-only finding, got:\n%s", got)
	}
}
