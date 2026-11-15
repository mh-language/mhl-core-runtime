package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// `goto` in a plain `pipeline` is a lint error — the linear-order guarantee
// is the whole reason `pipeline` and `workflow` are separate keywords.
func TestGotoInPipelineIsRejected(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step A { goto B }
    step B { log("b") }
}
`)
	findings := lint.File(main)
	if !hasMessage(findings, "`goto` is only valid inside a `workflow`") {
		t.Fatalf("expected a goto-in-pipeline finding, got %+v", findings)
	}
}

// The same source is clean once the keyword is `workflow`.
func TestGotoInWorkflowIsAllowed(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
workflow W {
    step A { goto B }
    step B { log("b") }
}
`)
	for _, f := range lint.File(main) {
		if strings.Contains(f.Message, "goto") {
			t.Fatalf("unexpected goto finding for a workflow: %+v", f)
		}
	}
}

func TestGotoMatchChecksTargetsAndPatterns(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `workflow W {
  step A {
    goto match artifact {
      "brief" -> B
      "brief" -> Missing
      _ -> fail("unknown")
      "adr" -> B
    }
  }
  step B {}
}`)
	findings := lint.File(main)
	for _, want := range []string{"targets a step that isn't declared", "duplicate goto match pattern", "follows the `_` wildcard"} {
		if !hasMessage(findings, want) {
			t.Errorf("missing %q in %+v", want, findings)
		}
	}
}

func TestWorkflowRouteChecksTargetAndArity(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `workflow W {
  route Generate(artifact: string) { "brief" -> Missing }
  step Gate { goto Generate() }
}`)
	findings := lint.File(main)
	for _, want := range []string{"targets step \"Missing\"", "expects 1 argument(s), got 0"} {
		if !hasMessage(findings, want) {
			t.Errorf("missing %q in %+v", want, findings)
		}
	}
}

// `goto nameof(B)` — the nameof-wrapped spelling — is checked exactly like
// the bare `goto B` form: clean when B is a declared step.
func TestGotoNameofWrappedTargetIsAllowed(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
workflow W {
    step A { goto nameof(B) }
    step B { log("b") }
}
`)
	for _, f := range lint.File(main) {
		if strings.Contains(f.Message, "goto") {
			t.Fatalf("unexpected goto finding for goto nameof(B): %+v", f)
		}
	}
}

// A nameof-wrapped target that names no declared step is caught the same
// way an unwrapped one is.
func TestGotoNameofWrappedUnknownTargetIsRejected(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
workflow W {
    step A { goto nameof(Nope) }
    step B { log("b") }
}
`)
	if !hasMessage(lint.File(main), "targets a step that isn't declared in it") {
		t.Fatalf("expected an unknown-goto-target finding for goto nameof(Nope)")
	}
}

// A `goto` target that names no step of the same declaration is caught
// statically rather than only at run time.
func TestGotoUnknownTargetInWorkflowIsRejected(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
workflow W {
    step A { goto Nope }
    step B { log("b") }
}
`)
	if !hasMessage(lint.File(main), "targets a step that isn't declared in it") {
		t.Fatalf("expected an unknown-goto-target finding")
	}
}

// `loop workflow` is still a workflow — `goto` stays legal under the `loop`
// prefix.
func TestGotoInLoopWorkflowIsAllowed(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
loop workflow W {
    repeat: { max_iterations: 3 }
    step A { goto B }
    step B { log("b") }
}
`)
	for _, f := range lint.File(main) {
		if strings.Contains(f.Message, "goto") {
			t.Fatalf("unexpected goto finding for a loop workflow: %+v", f)
		}
	}
}

// A `parallel` group is a goto target by its own name (the whole group
// runs); a branch step inside it is not; and the group's name shares the
// step namespace.
func TestGotoParallelGroup(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok.mh")
	write(t, ok, `
workflow W {
    parallel Fan {
        step L { log("l") }
        step R { log("r") }
    }
    step Gate { goto Fan }
    step Pick {
        goto match 1 {
            1 -> Fan
            _ -> fail("no")
        }
    }
}
`)
	for _, f := range lint.File(ok) {
		if strings.Contains(f.Message, "goto") || strings.Contains(f.Message, "Fan") {
			t.Fatalf("unexpected finding for goto to a parallel group: %+v", f)
		}
	}

	branch := filepath.Join(dir, "branch.mh")
	write(t, branch, `
workflow W {
    parallel Fan {
        step L { log("l") }
    }
    step Gate { goto L }
}
`)
	if !hasMessage(lint.File(branch), "targets a step inside a parallel group") {
		t.Fatalf("expected goto into a group's branch to be rejected")
	}

	dup := filepath.Join(dir, "dup.mh")
	write(t, dup, `
workflow W {
    step Fan { log("s") }
    parallel Fan {
        step L { log("l") }
    }
}
`)
	if !hasMessage(lint.File(dup), "more than one step or parallel group named \"Fan\"") {
		t.Fatalf("expected a step/group name clash finding, got %+v", lint.File(dup))
	}
}
