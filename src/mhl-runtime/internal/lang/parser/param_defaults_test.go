package parser

import (
	"strings"
	"testing"
)

// TestStringLiteralMatchingAnOperatorParses covers the regression where a
// string whose entire content is exactly an operator token (`"-"`, `"!"`,
// ...) was misread as that operator because participle.Unquote strips the
// quotes before literal matching. The `:Punct` pin on every operator rule
// in internal/lang/ast/expr.go is what fixes it.
func TestStringLiteralMatchingAnOperatorParses(t *testing.T) {
	for _, sep := range []string{`"-"`, `"!"`, `"+"`, `"=="`, `"<"`, `"*"`, `"&&"`, `"??"`} {
		src := `pipeline P { step S { var out = ["a", "b"].join(` + sep + `) } }`
		if _, err := Parse(src); err != nil {
			t.Errorf("join(%s): unexpected parse error: %v", sep, err)
		}
	}
}

// TestStringLiteralOperatorAsPrefixParses is the same class in unary
// position: `"-" + x` must read `"-"` as a string, not a prefix minus.
func TestStringLiteralOperatorAsPrefixParses(t *testing.T) {
	if _, err := ParseExpr(`"-" + suffix`); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
}

// TestMalformedNumberYieldsClearError covers a digit run glued to letters
// that is not a valid duration: it must fail at parse time with a
// "malformed number" message rather than silently splitting into a number
// and a bare identifier.
func TestMalformedNumberYieldsClearError(t *testing.T) {
	for _, lit := range []string{"10.0d", "1e5", "5days", "3.14pi"} {
		src := `pipeline P { step S { var x = ` + lit + ` } }`
		_, err := Parse(src)
		if err == nil {
			t.Errorf("%s: expected a parse error, got nil", lit)
			continue
		}
		if !strings.Contains(err.Error(), "malformed number") {
			t.Errorf("%s: expected a \"malformed number\" error, got: %v", lit, err)
		}
	}
}

// TestValidDurationsAndNumbersStillLex guards against the BadNumber rule
// stealing tokens it must not.
func TestValidDurationsAndNumbersStillLex(t *testing.T) {
	for _, lit := range []string{"7d", "30s", "500ms", "2h", "1m", "10", "10.5", "0"} {
		src := `pipeline P { step S { var x = ` + lit + ` } }`
		if _, err := Parse(src); err != nil {
			t.Errorf("%s: unexpected parse error: %v", lit, err)
		}
	}
}

// TestParamDefaultsParse confirms `name: type = expr` and `name = expr`
// param forms parse in tool methods, prompts and lambdas.
func TestParamDefaultsParse(t *testing.T) {
	src := `
tool T {
  greet(name: string, greeting: string = "Hello", loud: bool = false) -> greeting
}
prompt Pr(who: string, lang: string = "en") { "hi ${who} ${lang}" }
pipeline P {
  step S {
    var f = (a, b = 10, c = a + b) -> a + b + c
    var g = T.greet("x")
  }
}
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	m := prog.Decls[0].Tool.Methods[0]
	if m.Params[0].Default != nil {
		t.Errorf("param 0 should have no default")
	}
	if m.Params[1].Default == nil || m.Params[2].Default == nil {
		t.Errorf("params 1 and 2 should carry defaults")
	}
	pr := prog.Decls[1].Prompt
	if pr.Params[1].Default == nil {
		t.Errorf("prompt param 1 should carry a default")
	}
}
