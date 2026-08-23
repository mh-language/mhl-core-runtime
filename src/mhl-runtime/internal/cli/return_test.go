package cli_test

import (
	"strings"
	"testing"
)

// --- tool method block bodies + explicit return ---------------------------

func TestToolBlockBodyWithReturn(t *testing.T) {
	out, err := run(t, `
tool T {
    add(a, b) -> {
        var sum = a + b
        return sum
    }
}

`+wrapStep(`log(T.add(2, 3))`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "5\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

// TestToolBlockBodyImplicitVoid confirms a block that runs to completion
// with no `return` evaluates to nil — the same "null" formatValue already
// prints for any nil value.
func TestToolBlockBodyImplicitVoid(t *testing.T) {
	out, err := run(t, `
tool T {
    noop() -> {
        var x = 1
    }
}

`+wrapStep(`log(T.noop())`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "null\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestToolBlockBodyReturnInsideWhileStopsTheLoop(t *testing.T) {
	out, err := run(t, `
tool T {
    find_first_over(items, threshold) -> {
        var i = 0
        while (i < items.size()) {
            var v = items.get_index(i)
            if (v > threshold) {
                return v
            }
            i = i + 1
        }
        return -1
    }
}

`+wrapStep(`log(T.find_first_over([1, 2, 30, 40], 10))`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "30\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestToolBlockBodyReturnInsideIfElse(t *testing.T) {
	out, err := run(t, `
tool T {
    classify(n) -> {
        if (n > 10) {
            return "big"
        } else {
            return "small"
        }
    }
}

`+wrapStep(`
        log(T.classify(20))
        log(T.classify(1))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "big\n") || !strings.Contains(out, "small\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

// --- return interaction with try/catch/finally -----------------------

// TestReturnInsideTryBodyIsNotCaught confirms a `return` inside a try's
// Body skips Catch entirely (it's control flow, not an error to handle),
// but Finally still runs.
func TestReturnInsideTryBodyIsNotCaught(t *testing.T) {
	out, err := run(t, `
tool T {
    f() -> {
        try {
            return "from-try"
        } catch (e) {
            return "from-catch"
        } finally {
            log("finally-ran")
        }
    }
}

`+wrapStep(`log(T.f())`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "finally-ran\n") {
		t.Errorf("finally did not run: %s", out)
	}
	if !strings.Contains(out, "from-try\n") {
		t.Errorf("expected the try's own return value, catch must not have run: %s", out)
	}
	if strings.Contains(out, "from-catch") {
		t.Errorf("catch ran despite the body only returning, not erroring: %s", out)
	}
}

// TestReturnInsideCatchPropagates confirms a `return` reached via catch (an
// actual error was caught, and the catch block itself returns) also
// propagates correctly, with finally still running.
func TestReturnInsideCatchPropagates(t *testing.T) {
	out, err := run(t, `
tool T {
    f() -> {
        try {
            log(1 / 0)
        } catch (e) {
            return "caught-and-returned"
        } finally {
            log("finally-ran")
        }
    }
}

`+wrapStep(`log(T.f())`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "finally-ran\n") || !strings.Contains(out, "caught-and-returned\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

// TestReturnInsideTryFinallyErrorOverrides confirms Finally's own error
// still overrides an in-flight return, consistent with how Finally already
// overrides a normal error.
func TestReturnInsideTryFinallyErrorOverrides(t *testing.T) {
	_, err := run(t, `
tool T {
    f() -> {
        try {
            return "unreachable-result"
        } catch (e) {
            return "unreachable-catch"
        } finally {
            log(1 / 0)
        }
    }
}

`+wrapStep(`log(T.f())`))
	if err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("expected finally's error to override the pending return, got: %v", err)
	}
}

// --- bare `return` inside a pipeline step ------------------------------

func TestReturnInsideStepExitsEarlyWithoutError(t *testing.T) {
	out, err := run(t, wrapStep(`
        log("before")
        if (true) {
            return
        }
        log("unreachable")
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "before\n") {
		t.Errorf("unexpected output: %s", out)
	}
	if strings.Contains(out, "unreachable") {
		t.Errorf("statement after return still ran: %s", out)
	}
	if !strings.Contains(out, "executed 1 step(s)") {
		t.Errorf("expected the step to still count as completed: %s", out)
	}
}

func TestReturnWithValueInsideStepIsDiscarded(t *testing.T) {
	out, err := run(t, wrapStep(`
        return "some value"
        log("unreachable")
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out, "unreachable") {
		t.Errorf("statement after return still ran: %s", out)
	}
}
