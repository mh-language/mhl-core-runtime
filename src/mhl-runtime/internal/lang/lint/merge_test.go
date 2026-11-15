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
		{"break", `break`, "`break` cannot be used inside the merge of parallel group \"Fan\""},
		{"pause", `pause("wait")`, "`pause()` cannot be used inside the merge"},
		{"goto a branch", `goto Left`, "`goto Left` targets a step inside a parallel group"},
		{"goto an undeclared step", `goto Nowhere`, "`goto Nowhere` in workflow \"W\" targets a step that isn't declared"},
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

// A merge runs after the barrier, so it may leave the group like a step:
// `goto` (to a step, or back to the group itself) and `complete()`.
func TestParallelMergeMayGotoAndComplete(t *testing.T) {
	fs := lintSource(t, `
workflow W {
    var reply = []
    var rounds = 0
    parallel Fan {
        step Left  { self.reply = ["a"] }
        step Right { self.reply = ["b"] }
        merge (branches) {
            self.reply = [...branches.Left.reply, ...branches.Right.reply]
            self.rounds += 1
            if (self.rounds < 2) { goto Fan }
            if (self.reply.is_empty()) { complete() }
            goto Done
        }
    }
    step Done { log("done") }
}
`)
	if len(fs) != 0 {
		t.Fatalf("unexpected findings:\n%s", joined(fs))
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
// native namespace's method, and an assigned result, are left alone. A bare
// `x.append(v)` is not discarded: it runs as `x = x.append(v)`.
func TestDiscardedValueMethodResult(t *testing.T) {
	got := joined(lintSource(t, `
memory notes { type: "json" path: "notes.json" }

workflow W {
    var reply = []
    step S {
        self.reply.unique()
        var name = " a "
        name.trim()
        self.reply.append("x")
        reply = reply.append("ok")
        name = name.trim()
        notes.remove("k")
        log.info("fine")
    }
}
`))
	for _, want := range []string{
		"the result of self.reply.unique(...) is discarded: unique() returns a new array",
		"the result of name.trim(...) is discarded",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "is discarded"); n != 2 {
		t.Errorf("want exactly 2 discarded-result findings, got %d:\n%s", n, got)
	}
	if strings.Contains(got, "notes") {
		t.Errorf("a memory's own method was flagged:\n%s", got)
	}
}

// `x.append(v)` as a statement assigns x, so it is refused on a const or an
// input like any other assignment.
func TestAppendSugarOnReadOnlyNames(t *testing.T) {
	got := joined(lintSource(t, `
workflow W {
    input tags: string[] = []
    const FIXED = []
    step S {
        FIXED.append(1)
        tags.append("x")
    }
}
`))
	for _, want := range []string{`cannot assign to constant "FIXED"`, `cannot assign to input "tags"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// A group's own vars are in scope in its branches and its merge (with or
// without a param), and only there.
func TestParallelGroupVarsScope(t *testing.T) {
	fs := lintSource(t, `
workflow W {
    var reply = []
    parallel Fan {
        var left = ""
        var right = left
        step Left  { self.left = "a" }
        step Right { right = "b" }
        merge {
            self.reply.append(self.left)
            self.reply.append(right)
        }
    }
}
`)
	if len(fs) != 0 {
		t.Fatalf("unexpected findings:\n%s", joined(fs))
	}

	got := joined(lintSource(t, `
workflow W {
    parallel Fan {
        var left = ""
        step Left { self.left = "a" }
    }
    step After { self.left = "b" }
}
`))
	if !strings.Contains(got, "self.left: not a declared input, var, or mem") {
		t.Fatalf("a group var written after the group should be undeclared, got:\n%s", got)
	}
}

func TestParallelGroupVarsRules(t *testing.T) {
	cases := []struct{ name, group, want string }{
		{"shadows a workflow var", `var reply = []`, `var "reply" of parallel group "Fan" shadows the var "reply" of workflow "W"`},
		{"declared twice", "var a = 1\n        var a = 2", `parallel group "Fan" declares var "a" more than once`},
		{"undefined receiver in initializer", `var a = Missing.pick()`, `Missing`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := joined(lintSource(t, `
workflow W {
    var reply = []
    parallel Fan {
        `+c.group+`
        step Left { log("l") }
    }
}
`))
			if !strings.Contains(got, c.want) {
				t.Fatalf("want a finding containing %q, got:\n%s", c.want, got)
			}
		})
	}
	got := joined(lintSource(t, `
workflow W {
    parallel Fan {
        var b = 1
        step Left { log("l") }
        merge (b) { log("m") }
    }
}
`))
	if !strings.Contains(got, `parameter "b" is also a var of the group`) {
		t.Fatalf("merge param vs group var: got:\n%s", got)
	}
}
