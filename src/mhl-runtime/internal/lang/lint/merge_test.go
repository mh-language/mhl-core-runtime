package lint_test

import (
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// joined is every finding's message, one per line.
func joined(fs []lint.Finding) string {
	return strings.Join(messages(fs), "\n")
}

// A well-formed merge lints clean: its param is in scope, and the workflow's
// vars are readable and assignable in it.
func TestParallelMergeClean(t *testing.T) {
	fs := lintSource(t, `
workflow W {
    var reply = []
    parallel Fan {
        step Left  { self.reply = ["a"] }
        step Right { self.reply = ["b"] }
        merge (branches) {
            var both = [...branches.Left.reply, ...branches.Right.reply]
            self.reply = both
        }
    }
}
`)
	if len(fs) != 0 {
		t.Fatalf("unexpected findings:\n%s", joined(fs))
	}
}

func TestParallelMergeRules(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"goto", `goto Done`, "`goto` cannot be used inside the merge of parallel group \"Fan\""},
		{"break", `break`, "`break` cannot be used inside the merge of parallel group \"Fan\""},
		{"pause", `pause("wait")`, "`pause()` cannot be used inside the merge"},
		{"complete", `complete()`, "`complete()` cannot be used inside the merge"},
		{"unknown branch", `self.reply = b.Middle.reply`, `b.Middle: parallel group "Fan" has no step "Middle" (its steps: Left, Right)`},
		{"param is read-only", `b = {}`, `cannot assign to constant "b"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := joined(lintSource(t, `
workflow W {
    var reply = []
    parallel Fan {
        step Left  { self.reply = ["a"] }
        step Right { self.reply = ["b"] }
        merge (b) {
            `+c.body+`
        }
    }
    step Done { log("done") }
}
`))
			if !strings.Contains(got, c.want) {
				t.Fatalf("want a finding containing %q, got:\n%s", c.want, got)
			}
		})
	}
}

func TestParallelMergeParamShadowing(t *testing.T) {
	got := joined(lintSource(t, `
workflow W {
    var reply = []
    parallel Fan {
        step Left  { self.reply = ["a"] }
        merge (reply) { log("x") }
    }
}
`))
	if !strings.Contains(got, `parameter "reply" shadows the var "reply" of workflow "W"`) {
		t.Fatalf("want a shadowing finding, got:\n%s", got)
	}
}

// A value method called only for its result, which then goes nowhere, is a
// no-op — values are never aliased — and lint says so; a declaration's or a
// native namespace's method, and an assigned result, are left alone.
func TestDiscardedValueMethodResult(t *testing.T) {
	got := joined(lintSource(t, `
memory notes { type: "kv" path: "notes.json" }

workflow W {
    var reply = []
    step S {
        self.reply.append("x")
        var name = " a "
        name.trim()
        reply = reply.append("ok")
        name = name.trim()
        notes.remove("k")
        log.info("fine")
    }
}
`))
	for _, want := range []string{
		"the result of self.reply.append(...) is discarded",
		"the result of name.trim(...) is discarded",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "is discarded"); n != 2 {
		t.Errorf("want exactly 2 discarded-result findings, got %d:\n%s", n, got)
	}
}
