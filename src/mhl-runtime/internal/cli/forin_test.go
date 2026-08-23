package cli_test

import (
	"strings"
	"testing"
)

func TestForInIteratesArray(t *testing.T) {
	out, err := run(t, wrapStep(`
        for (var item in [10, 20, 30]) {
            log(item)
        }
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"10\n", "20\n", "30\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %s", want, out)
		}
	}
}

func TestForInIteratesObjectValues(t *testing.T) {
	out, err := run(t, wrapStep(`
        for (var item in [{id: 1}, {id: 2}]) {
            log(item.id)
        }
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "1\n") || !strings.Contains(out, "2\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestForInEmptyArrayRunsZeroTimes(t *testing.T) {
	out, err := run(t, wrapStep(`
        for (var item in []) {
            log("unreachable")
        }
        log("after")
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out, "unreachable") {
		t.Errorf("body ran for an empty array: %s", out)
	}
	if !strings.Contains(out, "after\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestForInNonArrayErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        for (var item in "not an array") {
            log(item)
        }
    `))
	if err == nil || !strings.Contains(err.Error(), "for-in requires an array, got string") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestForInVariableUsableAfterLoop(t *testing.T) {
	out, err := run(t, wrapStep(`
        for (var item in [1, 2, 3]) {
            log(item)
        }
        item = 99
        log(item)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "99\n") {
		t.Errorf("expected the loop variable to remain assignable after the loop (flat env), got: %s", out)
	}
}

func TestForInNested(t *testing.T) {
	out, err := run(t, wrapStep(`
        for (var row in [[1, 2], [3, 4]]) {
            for (var cell in row) {
                log(cell)
            }
        }
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"1\n", "2\n", "3\n", "4\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %s", want, out)
		}
	}
}

// TestForInReturnInsideToolMethodStopsEarly confirms return works through
// for-in exactly like it already does through while (same execBlock/error
// propagation channel, no special-casing needed for for-in).
func TestForInReturnInsideToolMethodStopsEarly(t *testing.T) {
	out, err := run(t, `
tool T {
    find_first_over(items, threshold) -> {
        for (var v in items) {
            if (v > threshold) {
                return v
            }
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

func TestForInExceedsMaxIterations(t *testing.T) {
	items := "[" + strings.Repeat("1,", 10_000) + "1]" // 10_001 elements
	_, err := run(t, wrapStep(`
        for (var x in `+items+`) {
        }
    `))
	if err == nil || !strings.Contains(err.Error(), "for-in exceeded the maximum of 10000 iterations") {
		t.Fatalf("unexpected error: %v", err)
	}
}
