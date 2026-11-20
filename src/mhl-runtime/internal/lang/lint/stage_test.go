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
