package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

func lintSrc(t *testing.T, src string) []lint.Finding {
	t.Helper()
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, src)
	return lint.File(main)
}

func TestMatchEnumNonExhaustiveFlagged(t *testing.T) {
	f := lintSrc(t, `
enum Status { Draft, Published, Archived }
pipeline P {
    step S {
        var s = Status.Draft
        var x = match s { Status.Draft -> "d" Status.Published -> "p" }
    }
}
`)
	if len(f) != 1 || !strings.Contains(f[0].Message, `match on enum "Status" is not exhaustive: missing Archived`) {
		t.Fatalf("unexpected findings: %+v", f)
	}
}

func TestMatchEnumExhaustiveOrWildcardIsClean(t *testing.T) {
	f := lintSrc(t, `
enum Status { Draft, Published, Archived }
pipeline P {
    step S {
        var s = Status.Draft
        var a = match s { Status.Draft -> 1 Status.Published -> 2 Status.Archived -> 3 }
        var b = match s { Status.Draft -> 1 _ -> 0 }
    }
}
`)
	if len(f) != 0 {
		t.Fatalf("expected no findings, got %+v", f)
	}
}

func TestMatchEnumViaInputTypeFlagged(t *testing.T) {
	f := lintSrc(t, `
enum Status { Draft, Published }
pipeline P {
    input st: Status
    step S { var x = match st { Status.Draft -> "d" } }
}
`)
	if len(f) != 1 || !strings.Contains(f[0].Message, "missing Published") {
		t.Fatalf("unexpected findings: %+v", f)
	}
}

func TestMatchBoolNonExhaustiveFlagged(t *testing.T) {
	f := lintSrc(t, `
pipeline P {
    step S {
        var b = true
        var x = match b { true -> "yes" }
    }
}
`)
	if len(f) != 1 || !strings.Contains(f[0].Message, "match on bool is not exhaustive: missing false") {
		t.Fatalf("unexpected findings: %+v", f)
	}
}

func TestMatchDuplicateAndUnreachableArmFlagged(t *testing.T) {
	f := lintSrc(t, `
pipeline P {
    step S {
        var x = match 1 { 1 -> "a" 1 -> "b" _ -> "c" 2 -> "d" }
    }
}
`)
	var msgs []string
	for _, x := range f {
		msgs = append(msgs, x.Message)
	}
	j := strings.Join(msgs, " | ")
	if !strings.Contains(j, "duplicate match pattern") || !strings.Contains(j, "unreachable") {
		t.Fatalf("unexpected findings: %v", msgs)
	}
}

func TestMatchInToolMethodBodyChecked(t *testing.T) {
	f := lintSrc(t, `
enum Status { Draft, Live }
tool T { label(s: Status): string -> match s { Status.Draft -> "d" } }
pipeline P { step S { log("x") } }
`)
	if len(f) != 1 || !strings.Contains(f[0].Message, "missing Live") {
		t.Fatalf("unexpected findings: %+v", f)
	}
}
