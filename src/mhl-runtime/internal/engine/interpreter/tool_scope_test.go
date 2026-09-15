package interpreter

import (
	"io"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

func TestToolScopeDeclarations(t *testing.T) {
	prog, err := parser.Parse(`
tool Counter {
    const base = 2
    var offset = base + 1
    value(n: number) -> {
        offset = offset + n
        return offset
    }
    fixed() -> base
    invalid() -> { base = 3 }
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ctx := &evalCtx{prog: prog, env: Env{}, out: io.Discard}
	for _, tc := range []struct {
		expr string
		want any
	}{
		{`Counter.value(4)`, float64(7)},
		{`Counter.value(4)`, float64(7)},
		{`Counter.fixed()`, float64(2)},
	} {
		expr, err := parser.ParseExpr(tc.expr)
		if err != nil {
			t.Fatalf("parse expression: %v", err)
		}
		got, err := evalExprAt(ctx, expr, 0)
		if err != nil || got != tc.want {
			t.Fatalf("%s = %v, %v; want %v", tc.expr, got, err, tc.want)
		}
	}
	expr, _ := parser.ParseExpr(`Counter.invalid()`)
	if _, err := evalExprAt(ctx, expr, 0); err == nil || !strings.Contains(err.Error(), `cannot assign to constant "base"`) {
		t.Fatalf("expected constant assignment error, got %v", err)
	}
}
