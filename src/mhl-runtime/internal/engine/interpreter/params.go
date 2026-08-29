package interpreter

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// requiredParamCount is ast.RequiredParamCount, re-exposed unqualified for
// the call sites in this package.
func requiredParamCount(params []*ast.Param) int { return ast.RequiredParamCount(params) }

// paramArityText renders the arity half of a "wrong number of arguments"
// error. When no parameter has a default (required == total) it reproduces
// the original "requires N argument(s)" wording verbatim; otherwise it
// states the accepted range.
func paramArityText(required, total int) string {
	if required == total {
		return fmt.Sprintf("requires %d argument(s)", total)
	}
	return fmt.Sprintf("requires between %d and %d argument(s)", required, total)
}
