package flow

import (
	"reflect"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

var postfixType = reflect.TypeOf(ast.Postfix{})

// decisions lists the decision types of body's gates — `pause(reason,
// decision: T)` calls, wherever they sit (`var d = pause(...)` included) —
// in source order. A gate returns its decision, so unlike a bare pause it is
// not an exit: the step goes on with the answer.
func decisions(body []*ast.Statement) []string {
	var out []string
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
			if v.Type() == postfixType {
				pf := v.Addr().Interface().(*ast.Postfix)
				if pf.Primary != nil && pf.Primary.Ident == "pause" && len(pf.Ops) > 0 && pf.Ops[0].Call != nil {
					for _, a := range pf.Ops[0].Call.Args {
						if a.Name == "decision" {
							if name, ok := ast.IdentValue(a.Value); ok {
								out = append(out, name)
							}
						}
					}
				}
			}
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		}
	}
	walk(reflect.ValueOf(body))
	return out
}
