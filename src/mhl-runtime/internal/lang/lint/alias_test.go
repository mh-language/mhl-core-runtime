package lint_test

import (
	"strings"
	"testing"
)

func TestWorkflowAliasChecks(t *testing.T) {
	got := messages(lintSrc(t, `
enum Level { discovery, delivery }
enum Other { x }
type Req = { level: Level, n?: number }
workflow Flow {
    input level: Level
    input n: number = 0
    step A { log("${level}") }
}
workflow Typed(req: Req) { step A { log("t") } }
pipeline P { step A { log("p") } }
workflow Ok1 = Flow with { level: Level.delivery, n: 2 } { description: "fine" }
workflow Ok2 = Flow with { level: "discovery" }
workflow Ok3 = Typed with { level: Level.delivery }
workflow A1 = Flow with { level: Level.nope }
workflow A2 = Flow with { level: Other.x, n: "tres" }
workflow A3 = Flow with { missing: 1 } { timeout: 3 }
workflow A4 = Ok1
workflow A5 = P
workflow A6 = Ghost
workflow A7 = Flow with { level: "nope" }
workflow A8 = Flow with { level: Level.delivery, level: Level.discovery }
workflow A9 = Flow with { level: env("X") }
`))
	want := []string{
		`"A1": bound input "level" names no variant of enum Level (use discovery | delivery)`,
		`"A2": bound input "level" must be Level, got enum Other`,
		`"A2": bound input "n" must be number, got string`,
		`"A3": an alias may only set ` + "`description`" + `, not "timeout"`,
		`"A3": workflow "Flow" has no input "missing" to bind`,
		`"A4": target "Ok1" is itself an alias`,
		`"A5": target "P" is a pipeline — an alias keeps its target's kind`,
		`"A6": target "Ghost" is not a declared pipeline or workflow`,
		`"A7": bound input "level" "nope" is not a variant of enum Level`,
		`"A8": input "level" is bound twice`,
		`"A9": bound input "level" must be a literal or an enum value`,
	}
	if len(got) != len(want) {
		t.Fatalf("want %d findings, got %d:\n%s", len(want), len(got), strings.Join(got, "\n"))
	}
	all := strings.Join(got, "\n")
	for _, w := range want {
		if !strings.Contains(all, w) {
			t.Errorf("missing a finding containing %q in:\n%s", w, all)
		}
	}
}
