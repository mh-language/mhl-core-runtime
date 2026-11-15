package parser

import (
	"strings"
	"testing"
)

// `...` parses only as a direct element of an array literal; anywhere else
// it is a parse error (checkSpreadPlacement), so it can never quietly mean
// something else.
func TestSpreadPlacement(t *testing.T) {
	cases := []struct {
		name, src string
		ok        bool
	}{
		{"array element", `["a", ...xs, "b"]`, true},
		{"several and nested", `[...a, [...b], ...(if (c) [] else ["--f", v])]`, true},
		{"spread binds looser than ??", `[...xs ?? []]`, true},
		{"call argument", `f(...xs)`, false},
		{"named argument", `f(items: ...xs)`, false},
		{"bare expression", `...xs`, false},
		{"object field", `{ k: ...xs }`, false},
		{"inside a parenthesis in an array", `[(...xs)]`, false},
		{"inside a lambda body", `[(x) -> ...x]`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseExpr(tc.src)
			if tc.ok && err != nil {
				t.Fatalf("ParseExpr(%s): %v", tc.src, err)
			}
			if !tc.ok && (err == nil || !strings.Contains(err.Error(), "only allowed as an element of an array literal")) {
				t.Fatalf("ParseExpr(%s) err = %v, want the placement error", tc.src, err)
			}
		})
	}
}

// The same check runs on a whole program (mhl lint and mhl run both parse
// through Parse), with the position of the offending spread.
func TestSpreadPlacementInProgram(t *testing.T) {
	_, err := Parse("pipeline P {\n    step S {\n        log(...xs)\n    }\n}\n")
	if err == nil || !strings.Contains(err.Error(), "3:13") {
		t.Fatalf("err = %v, want a placement error at 3:13", err)
	}
}

// `..` (slice range) still lexes on its own next to the new `...` token.
func TestSliceRangeUnaffectedBySpread(t *testing.T) {
	for _, src := range []string{`xs[1..]`, `xs[..2]`, `xs[1..2]`, `[...xs[1..]]`} {
		if _, err := ParseExpr(src); err != nil {
			t.Errorf("ParseExpr(%s): %v", src, err)
		}
	}
}
