package lint_test

import (
	"strings"
	"testing"
)

func TestStageInputChecks(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"a stage input of a stage that runs", `partial workflow W {
    input who: string
    entry step A { goto S }
}
partial workflow W stage S {
    input plan: string
    step B { log(base.who, plan) }
}`, ""},
		{"an input declared twice across fragments", `partial workflow W {
    input plan: string
    entry step A { goto S }
}
partial workflow W stage S {
    input plan: number
    step B { log(plan) }
}`, `declares input "plan" more than once`},
		{"a stage input in a stage with nothing to run", `partial workflow W {
    entry step A { log(1) }
}
partial workflow W stage S {
    input plan: string
}`, `the stage has no step or parallel group`},
		{"a stage input in a loop workflow", `partial loop workflow W max 2 {
    entry step A { goto S }
}
partial loop workflow W stage S {
    input plan: string
    step B { log(plan) }
}`, `a loop run cannot pause to ask for a stage's inputs yet`},
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
