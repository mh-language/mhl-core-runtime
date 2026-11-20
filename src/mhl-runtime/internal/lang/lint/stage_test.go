package lint_test

import (
	"strings"
	"testing"
)

func TestStageChecks(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"a header stage on a partial fragment, entered by goto", `partial workflow W stage Build {
    entry step A { goto Build }
}`, ""},
		{"a header stage needs a partial", `workflow W stage Build {
    step A { log(1) }
}`, "split the workflow into one `partial` fragment per phase"},
		{"name clash with a step", `partial workflow W stage A {
    entry step A { log(1) }
}`, `stage "A" of workflow "W" has the name of a step`},
		{"goto an unknown stage", `partial workflow W stage S {
    entry step A { goto Nope }
}`, "`goto Nope`"},
		{"the block form is gone", `workflow W {
    stage S { step A { log(1) } }
}`, "unexpected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var msgs []string
			for _, f := range lintSource(t, tc.src) {
				msgs = append(msgs, f.Message)
			}
			got := strings.Join(msgs, "\n")
			if tc.want == "" && got != "" {
				t.Fatalf("unexpected findings:\n%s", got)
			}
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, got)
			}
		})
	}
}

func TestStageEntryWarnings(t *testing.T) {
	// Two fragments of one workflow in one source, each its own stage.
	src := `partial workflow W {
    input existing: bool = false
    entry step Start {
        if (existing) { goto Approve }
        goto Spec
    }
}
partial workflow W stage Spec {
    step Draft { log("draft") }
    step Approve { goto Build }
}
partial workflow W stage Build {
    step Compile { log("c") }
    step Check {
        if (true) { goto Approve }
        goto Compile
    }
}`
	var warns, errs []string
	for _, f := range lintSource(t, src) {
		if f.IsWarning() {
			warns = append(warns, f.Message)
		} else {
			errs = append(errs, f.Message)
		}
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	// Start → Approve (outside any stage, mid-Spec) and Check → Approve
	// (from Build, mid-Spec) warn; goto Spec, goto Build (a stage) and
	// goto Compile (within Build) don't.
	if len(warns) != 2 {
		t.Fatalf("warnings = %q", warns)
	}
	if !strings.Contains(warns[0], "`goto Approve` from outside any stage enters stage \"Spec\" at Approve, not at its start (Draft) — prefer `goto Spec`") ||
		!strings.Contains(warns[1], "from stage \"Build\"") {
		t.Fatalf("warnings = %q", warns)
	}
}
