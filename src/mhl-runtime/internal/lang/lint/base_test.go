package lint_test

import (
	"strings"
	"testing"
)

// Two fragments of one workflow in one source: the base (entry step) and a
// stage reading the base's state.
const baseProgram = `partial workflow W {
    input dir: string
    var folder = ""
    entry step Load {
        self.folder = dir + "/x"
        goto Use
    }
}
partial workflow W stage Use {
    var mine = 0
    step Read {
        var local = 1
        log(STAGE_LINE)
    }
}`

func baseFindings(t *testing.T, line string) []string {
	t.Helper()
	var msgs []string
	for _, f := range lintSource(t, strings.Replace(baseProgram, "STAGE_LINE", line, 1)) {
		sev := "error"
		if f.Severity != "" {
			sev = string(f.Severity)
		}
		msgs = append(msgs, sev+": "+f.Message)
	}
	return msgs
}

func TestBaseRefs(t *testing.T) {
	cases := []struct{ name, line, want string }{
		{"base.x of the base's var", `base.folder`, ""},
		{"base.x of the base's input", `base.dir`, ""},
		{"a name of its own and a local stay bare", `mine + local`, ""},
		{"a bare name from the base", `folder`, "warning: folder is declared in the base fragment"},
		{"self.x of the base's name", `self.folder`, "warning: self.folder is declared in the base fragment"},
		{"inside a string", `"in ${dir}"`, "warning: dir is declared in the base fragment"},
		{"base.x the base does not declare", `base.mine`, `error: base.mine: the base fragment (the one with the entry step) declares no input, var, mem or const "mine"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(baseFindings(t, tc.line), "\n")
			if tc.want == "" && got != "" {
				t.Fatalf("unexpected findings:\n%s", got)
			}
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, got)
			}
		})
	}
}

func TestBaseRefOutsideAPartialWorkflow(t *testing.T) {
	src := `workflow W {
    var n = 1
    step S { log(base.n) }
}`
	var got []string
	for _, f := range lintSource(t, src) {
		got = append(got, f.Message)
	}
	if !strings.Contains(strings.Join(got, "\n"), "is not partial; write n") {
		t.Fatalf("findings = %v", got)
	}
	// A variable named base keeps meaning the variable.
	ok := `workflow W {
    step S {
        var base = {n: 1}
        log(base.n)
    }
}`
	if f := lintSource(t, ok); len(f) != 0 {
		t.Fatalf("a var named base: %v", f)
	}
}
