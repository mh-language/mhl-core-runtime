package lint_test

import (
	"strings"
	"testing"
)

func TestCheckConstReassignFlagged(t *testing.T) {
	f := lintSrc(t, `
pipeline P {
    step S {
        const k = 1
        k = 2
    }
}
`)
	if len(f) != 1 || !strings.Contains(f[0].Message, `cannot assign to constant "k"`) {
		t.Fatalf("unexpected findings: %+v", f)
	}
}

func TestCheckConstPlusEqualsFlagged(t *testing.T) {
	f := lintSrc(t, `
pipeline P {
    const C = 1
    step S { C += 1 }
}
`)
	if len(f) != 1 || !strings.Contains(f[0].Message, `cannot assign to constant "C"`) {
		t.Fatalf("unexpected findings: %+v", f)
	}
}

func TestCheckConstRedeclaredByVarFlagged(t *testing.T) {
	f := lintSrc(t, `
pipeline P {
    step S {
        const x = 1
        var x = 2
    }
}
`)
	if len(f) != 1 || !strings.Contains(f[0].Message, `"x" is already declared as a constant`) {
		t.Fatalf("unexpected findings: %+v", f)
	}
}

func TestCheckConstReadIsClean(t *testing.T) {
	f := lintSrc(t, `
pipeline P {
    const LIMIT = 3
    step S {
        const name = "w"
        var total = LIMIT + 1
        log(name)
        log(total)
    }
}
`)
	if len(f) != 0 {
		t.Fatalf("expected no findings, got %+v", f)
	}
}
