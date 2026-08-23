package cli_test

import (
	"strings"
	"testing"
)

// TestClosureLinqWhereEndToEnd is the motivating example: a tool method
// taking a closure parameter and calling it as a value (`predicate(item)`),
// with an inline lambda passed at the call site.
func TestClosureLinqWhereEndToEnd(t *testing.T) {
	out, err := run(t, `
tool Linq {
    where(items, predicate) -> {
        var result = []
        for (var item in items) {
            if (predicate(item)) {
                result = result + [item]
            }
        }
        return result
    }
}

`+wrapStep(`
        var items = [{id: 1, passes: true}, {id: 2, passes: false}, {id: 3, passes: true}]
        var filtered = Linq.where(items, (item) -> item.passes == true)
        log(filtered.size())
        log(filtered)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "2\n") {
		t.Errorf("expected 2 filtered items, got: %s", out)
	}
	if !strings.Contains(out, `"id":1`) || !strings.Contains(out, `"id":3`) || strings.Contains(out, `"id":2`) {
		t.Errorf("unexpected filtered content: %s", out)
	}
}

func TestClosureStoredInVariableAndCalled(t *testing.T) {
	out, err := run(t, wrapStep(`
        var double = (x) -> x * 2
        log(double(21))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "42\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestClosureCapturesOuterVariableByValue(t *testing.T) {
	out, err := run(t, wrapStep(`
        var factor = 10
        var scale = (x) -> x * factor
        factor = 999
        log(scale(3))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "30\n") {
		t.Errorf("expected the closure to see factor=10 (captured at creation, not live), got: %s", out)
	}
}

func TestClosureBlockBodyWithReturn(t *testing.T) {
	out, err := run(t, wrapStep(`
        var classify = (n) -> {
            if (n > 10) {
                return "big"
            }
            return "small"
        }
        log(classify(20))
        log(classify(1))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "big\n") || !strings.Contains(out, "small\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestClosureZeroArg(t *testing.T) {
	out, err := run(t, wrapStep(`
        var greet = () -> "hi"
        log(greet())
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "hi\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestClosureMultiArg(t *testing.T) {
	out, err := run(t, wrapStep(`
        var add = (a, b) -> a + b
        log(add(2, 3))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "5\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestClosureWrongArgCountErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var add = (a, b) -> a + b
        log(add(1))
    `))
	if err == nil || !strings.Contains(err.Error(), "closure requires 2 argument(s), got 1") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallingNonClosureValueStillErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var x = 1
        log(x())
    `))
	if err == nil || !strings.Contains(err.Error(), "value is not callable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClosureTypeNameIsFunction(t *testing.T) {
	_, err := run(t, wrapStep(`
        var f = (x) -> x
        log(f + 1)
    `))
	if err == nil || !strings.Contains(err.Error(), "got function") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClosureFormatValueIsPlaceholder(t *testing.T) {
	out, err := run(t, wrapStep(`
        var f = (x) -> x
        log(f)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "<function>\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestArrayConcatenationViaPlus(t *testing.T) {
	out, err := run(t, wrapStep(`
        var a = [1, 2]
        var b = [3, 4]
        log(a + b)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "[1,2,3,4]") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestArrayPlusNonArrayErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var a = [1, 2]
        log(a + "x")
    `))
	if err == nil || !strings.Contains(err.Error(), "requires both operands to be arrays") {
		t.Fatalf("unexpected error: %v", err)
	}
}
