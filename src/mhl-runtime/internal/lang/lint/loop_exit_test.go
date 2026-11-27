package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// A loop needs a way to stop: `max N`, a `break` in a step, or (deprecated)
// a repeat ceiling / stop_when. `complete()` only ends one iteration.
func TestLoopWithoutExitIsWarned(t *testing.T) {
	const noExit = "has no way to stop"
	cases := []struct {
		name, src string
		warn      bool
	}{
		{"no exit", `loop workflow W {
    step S { log("x") }
}`, true},
		{"complete only ends an iteration", `loop workflow W {
    step S { complete() }
}`, true},
		{"max", `loop workflow W max 3 {
    step S { log("x") }
}`, false},
		{"break nested in if", `loop workflow W {
    mem n = 0
    step S {
        self.n = n + 1
        if (n >= 2) { break "done" }
    }
}`, false},
		{"deprecated stop_when still counts", `loop workflow W {
    mem n = 0
    repeat: { stop_when: n >= 2 }
    step S { self.n = n + 1 }
}`, false},
		{"not a loop", `workflow W {
    step S { log("x") }
}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			main := filepath.Join(t.TempDir(), "main.mh")
			write(t, main, tc.src+"\n")
			var got []lint.Finding
			for _, f := range lint.File(main) {
				if strings.Contains(f.Message, noExit) {
					got = append(got, f)
				}
			}
			if tc.warn && (len(got) != 1 || got[0].Severity != lint.SeverityWarning) {
				t.Fatalf("want one %q warning, got %+v", noExit, lint.File(main))
			}
			if !tc.warn && len(got) != 0 {
				t.Fatalf("want no %q warning, got %+v", noExit, got)
			}
		})
	}
}

// `repeat: { ... }` is deprecated; the warning names the replacement for
// each field the block sets.
func TestRepeatBlockIsDeprecated(t *testing.T) {
	cases := []struct {
		block string
		want  []string
		not   []string
	}{
		{`repeat: { max_iterations: 3 }`, []string{"`loop workflow X max N`"}, []string{"break"}},
		{`repeat: { stop_when: n >= 2 }`, []string{"`break`"}, []string{"max N"}},
		{`repeat: { stop_when: n >= 2, max_iterations: 3 }`, []string{"max N", "`break`"}, nil},
	}
	for _, tc := range cases {
		main := filepath.Join(t.TempDir(), "main.mh")
		write(t, main, "loop workflow W {\n    mem n = 0\n    "+tc.block+"\n    step S { self.n = n + 1 }\n}\n")
		var dep []lint.Finding
		for _, f := range lint.File(main) {
			if strings.Contains(f.Message, "`repeat: { ... }` is deprecated") {
				dep = append(dep, f)
			}
		}
		if len(dep) != 1 || dep[0].Severity != lint.SeverityWarning {
			t.Fatalf("%s: want one deprecation warning, got %+v", tc.block, lint.File(main))
		}
		for _, w := range tc.want {
			if !strings.Contains(dep[0].Message, w) {
				t.Errorf("%s: message %q lacks %q", tc.block, dep[0].Message, w)
			}
		}
		for _, w := range tc.not {
			if strings.Contains(dep[0].Message, w) {
				t.Errorf("%s: message %q should not mention %q", tc.block, dep[0].Message, w)
			}
		}
	}
}
