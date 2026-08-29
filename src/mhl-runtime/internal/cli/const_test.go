package cli_test

import (
	"strings"
	"testing"
)

func TestConstReadableAndUsable(t *testing.T) {
	out, err := run(t, `
pipeline P {
    const LIMIT = 3
    step S {
        const label = "widget"
        log(label)
        log(LIMIT + 1)
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "widget\n") || !strings.Contains(out, "4\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestConstReassignErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        const k = 1
        k = 2
    `))
	if err == nil || !strings.Contains(err.Error(), `cannot assign to constant "k"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConstPlusEqualsErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        const total = 10
        total += 1
    `))
	if err == nil || !strings.Contains(err.Error(), `cannot assign to constant "total"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPipelineConstIsReadOnlyInSteps(t *testing.T) {
	_, err := run(t, `
pipeline P {
    const C = 1
    step S { C = 2 }
}
`)
	if err == nil || !strings.Contains(err.Error(), `cannot assign to constant "C"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConstInLoopBodyRebinds(t *testing.T) {
	out, err := run(t, wrapStep(`
        var i = 0
        while (i < 3) {
            const msg = "n=${i}"
            log(msg)
            i = i + 1
        }
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"n=0\n", "n=1\n", "n=2\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
}
