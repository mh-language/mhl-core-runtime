package lint

import (
	"fmt"
	"reflect"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// checkSpreadLiterals reports a `...` spread whose operand is a literal that
// is not an array — `[...42]`, `["a", ..."b"]`, `[...null]` — which
// interpreter.evalSpread always rejects ("spread (...) needs an array, got
// number"). A computed operand is only known at run time.
func checkSpreadLiterals(file string, prog *ast.Program) []Finding {
	var findings []Finding
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Ptr, reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Struct:
			if v.Type() == reflect.TypeOf(ast.Array{}) {
				for _, item := range v.Addr().Interface().(*ast.Array).Items {
					if v, literal := spreadOperandLiteral(item); literal {
						if _, isArray := v.([]any); !isArray {
							findings = append(findings, spreadFinding(file, item.Pos,
								fmt.Sprintf("spread (...) needs an array, got %s", literalTypeName(v))))
						}
					}
				}
			}
			t := v.Type()
			for i := 0; i < v.NumField(); i++ {
				if t.Field(i).PkgPath == "" {
					walk(v.Field(i))
				}
			}
		}
	}
	walk(reflect.ValueOf(prog))
	return findings
}

// spreadOperandLiteral reads the operand of a `...expr` item as a literal
// (ast.LiteralValue refuses a spread item itself, which stands for zero or
// more values). ok is false for a non-spread item or a computed operand.
func spreadOperandLiteral(item *ast.Expr) (any, bool) {
	if item == nil || !item.Spread {
		return nil, false
	}
	operand := *item
	operand.Spread = false
	return ast.LiteralValue(&operand)
}

func spreadFinding(file string, pos lexer.Position, msg string) Finding {
	return Finding{File: fileAt(file, pos), Line: pos.Line, Column: pos.Column, Message: msg}
}

// literalTypeName names a literal value's type the way the interpreter's
// typeName does in its runtime errors.
func literalTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "bool"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}
