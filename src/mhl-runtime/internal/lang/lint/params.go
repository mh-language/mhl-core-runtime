package lint

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// paramArityText renders the arity half of a "wrong number of arguments"
// finding, matching internal/engine/interpreter.paramArityText verbatim so
// a lint message and the run-time message read identically.
func paramArityText(required, total int) string {
	if required == total {
		return fmt.Sprintf("requires %d argument(s)", total)
	}
	return fmt.Sprintf("requires between %d and %d argument(s)", required, total)
}

// checkParamDefaults enforces the one structural rule default values add:
// in a positionally-bound parameter list (tool methods; `prompt` binds by
// name and is exempt) a parameter without a default may not follow one that
// has a default, since a caller could then never omit the earlier argument
// while supplying the later one.
func checkParamDefaults(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		if decl.Tool == nil {
			continue
		}
		for _, m := range decl.Tool.Methods {
			seenDefault := false
			for _, p := range m.Params {
				switch {
				case p.Default != nil:
					seenDefault = true
				case seenDefault:
					findings = append(findings, Finding{
						File:    file,
						Line:    p.Pos.Line,
						Column:  p.Pos.Column,
						Message: fmt.Sprintf("tool %q: %s: parameter %q has no default but follows a defaulted parameter", decl.Tool.Name, m.Name, p.Name),
					})
				}
			}
		}
	}
	return findings
}
