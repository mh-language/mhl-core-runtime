package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

func lintHookSource(t *testing.T, src string) []lint.Finding {
	t.Helper()
	main := filepath.Join(t.TempDir(), "main.mh")
	write(t, main, src)
	return lint.File(main)
}

// `on <event>` with or without its parameter is clean.
func TestOnHooksLintClean(t *testing.T) {
	findings := lintHookSource(t, `
workflow P {
    mem ends = 0
    on session_start(session) { log("start ${session.pipeline}") }
    on step_end { ends = ends + 1 }
    on stop_failure(failure: FailureContext) { log(failure.reason) }
    step One { }
}
`)
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

// The property spelling still works, with a warning naming the new one.
func TestPropertyHookIsDeprecated(t *testing.T) {
	findings := lintHookSource(t, `
workflow P {
    session_end: (s) -> { log("end") }
    step One { }
}
`)
	if len(findings) != 1 || findings[0].Severity != lint.SeverityWarning ||
		!strings.Contains(findings[0].Message, "`on session_end(session) { ... }`") {
		t.Fatalf("want one deprecation warning naming `on session_end`, got %+v", findings)
	}
}

func TestOnHookMistakes(t *testing.T) {
	cases := []struct{ name, member, want string }{
		{"unknown event", `on stop_failur(f) { }`, "`on stop_failur`: not a lifecycle event"},
		{"a property is not an event", `on description { }`, "`on description`: not a lifecycle event"},
		{"two parameters", `on step_start(a, b) { }`, "`on step_start` takes at most one parameter"},
		{"handled twice", "on step_end { }\n    on step_end { }", "step_end is handled twice"},
		{"handled twice across spellings", "on step_end { }\n    step_end: (s) -> { }", "step_end is handled twice"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			findings := lintHookSource(t, "workflow P {\n    "+c.member+"\n    step One { }\n}\n")
			for _, f := range findings {
				if f.Severity != lint.SeverityWarning && strings.Contains(f.Message, c.want) {
					return
				}
			}
			t.Fatalf("want an error containing %q, got %+v", c.want, findings)
		})
	}
}
