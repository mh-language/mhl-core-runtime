package parser

import (
	"strings"
	"testing"
)

// TestAmbiguousBareReturnIsRejected is the regression test for the
// reported bug: `return`/`break` with nothing after it on the same line
// used to silently swallow the following statement as its own value —
// `if (flag) return` immediately followed by more code parsed as one
// statement, "if flag, return the result of (and side effect of) calling
// the next line", not "return early; otherwise run the next line". Both
// shapes must now be a clear parse error instead.
func TestAmbiguousBareReturnIsRejected(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "inline if + return, followed by more code",
			src: `tool T {
    m(flag: bool) -> {
        if (flag) return
        log.info("x")
    }
}`,
		},
		{
			name: "plain guard-clause return with no if at all",
			src: `tool T {
    m() -> {
        return
        log.info("x")
    }
}`,
		},
		{
			name: "break with the same shape, inside a while loop",
			src: `tool T {
    m() -> {
        while (true) {
            if (true) break
            log.info("x")
        }
    }
}`,
		},
		{
			name: "swallow bug inside a lambda passed as a call argument",
			src: `tool T {
    m(items: any[]) -> {
        return items.filter((x) -> {
            if (x) return
            log.info("x")
        })
    }
}`,
		},
		{
			name: "swallow bug inside a pipeline step",
			src: `pipeline P {
    step s {
        if (true) return
        log.info("x")
    }
}`,
		},
		{
			name: "swallow bug inside a hook lambda (a Property value, not a Block)",
			src: `pipeline P {
    session_start: (s) -> {
        if (s.resumed) return
        log.info("x")
    }
    step s {}
}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.src)
			if err == nil {
				t.Fatalf("expected a parse error, got none")
			}
			if !strings.Contains(err.Error(), "own value") {
				t.Errorf("error = %q, want it to mention the ambiguous-value rule", err.Error())
			}
		})
	}
}

// TestLegitimateReturnsAndBreaksStillParse proves the check doesn't
// misfire on any of the genuinely valid shapes: a same-line value, a
// multi-line value whose own first token starts right after the keyword,
// a braced inline-if branch, a bare return/break with nothing following
// it at all, and one used as the very last statement of its block.
func TestLegitimateReturnsAndBreaksStillParse(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "same-line return value",
			src: `tool T {
    m(): number -> {
        return 1 + 2
    }
}`,
		},
		{
			name: "multi-line object literal opening on the same line",
			src: `tool T {
    m(): object -> {
        return {
            a: 1,
            b: 2
        }
    }
}`,
		},
		{
			name: "inline if with return braced",
			src: `tool T {
    m(flag: bool) -> {
        if (flag) { return }
        log.info("x")
    }
}`,
		},
		{
			name: "bare return with nothing following it in the block",
			src: `tool T {
    m() -> {
        log.info("x")
        return
    }
}`,
		},
		{
			name: "break as a loop's last statement",
			src: `tool T {
    m() -> {
        while (true) {
            break
        }
    }
}`,
		},
		{
			name: "return with no value, block ends right there",
			src: `tool T {
    m(flag: bool) -> {
        if (flag) {
            return
        }
    }
}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.src); err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}
