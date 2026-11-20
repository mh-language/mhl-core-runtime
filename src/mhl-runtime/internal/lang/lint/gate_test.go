package lint_test

import (
	"strings"
	"testing"
)

func TestGateChecks(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"a declared type", `type R = { ok: bool }
workflow W { step A { var d = pause("x", decision: R) } }`, ""},
		{"a builtin type", `workflow W { step A { var d = pause("x", decision: bool) } }`, ""},
		{"unknown type", `workflow W { step A { var d = pause("x", decision: Nope) } }`, `"Nope" is not a declared type or enum`},
		{"unknown argument", `workflow W { step A { pause("x", choice: 1) } }`, `unknown argument "choice"`},
		{"not in a loop", `type R = { ok: bool }
loop workflow L max 2 { step A { var d = pause("x", decision: R) } }`, "a resume can't answer a gate in a loop yet"},
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
