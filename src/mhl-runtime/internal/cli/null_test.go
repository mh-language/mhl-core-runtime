package cli_test

import (
	"strings"
	"testing"
)

func TestNullLiteralLogsAsNull(t *testing.T) {
	out, err := run(t, wrapStep(`
        var x = null
        log(x)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "null\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestNullEqualityComparisons(t *testing.T) {
	out, err := run(t, wrapStep(`
        log(null == null)
        log(null != null)
        var x = 1
        log(x == null)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "true\n") || !strings.Contains(out, "false\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

// TestNullMatchesImplicitVoid confirms the literal `null` and the implicit
// void a return-less block/closure already produced (internal/engine/interpreter/tool.go,
// closure.go) are the same value — so `f() == null` is a natural way to
// check "this callable returned nothing".
func TestNullMatchesImplicitVoid(t *testing.T) {
	out, err := run(t, wrapStep(`
        var f = () -> null
        var g = () -> {
            var x = 1
        }
        log(f() == null)
        log(g() == null)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Count(out, "true\n") != 2 {
		t.Errorf("expected both to equal null, got: %s", out)
	}
}

func TestNullAsMemoryDefaultValue(t *testing.T) {
	out, err := run(t, `
memory session_mem {
    type: "kv"
    store: "memory"
}

`+wrapStep(`
        var v = session_mem.get("missing", null)
        log(v == null)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "true\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestNullOperatorTypeMismatchErrors(t *testing.T) {
	_, err := run(t, wrapStep(`log(null + 1)`))
	if err == nil || !strings.Contains(err.Error(), "got null") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNullNotTreatedAsUndefinedVariable(t *testing.T) {
	_, err := run(t, wrapStep(`log(null)`))
	if err != nil {
		t.Fatalf("expected null to be a literal, not an undefined variable, got: %v", err)
	}
}
