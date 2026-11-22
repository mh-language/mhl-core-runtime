package parser

import (
	"reflect"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

var statementType = reflect.TypeOf(ast.Statement{})

// attachGateDocs names and documents each typed gate a statement binds:
//
//	/// approved: true publishes the proposal, false discards it
//	var approved = pause("Review the proposal", decision: bool)
//
// The gate's call gets Gate{Name: "approved", Doc: "true publishes …"} — the
// `///` lines right above the statement, read as attachInputDocs reads an
// input's (a leading "approved:" dropped). The name is the bound one: a
// `var`/`const`, or the last member of an assignment target (`self.ok =`,
// `base.ok =`). A gate in any other position (an argument, an `if`
// condition) keeps no name.
func attachGateDocs(prog *ast.Program, source string) {
	var lines []string
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
			if v.Type() == statementType {
				st := v.Addr().Interface().(*ast.Statement)
				if name, call := gateBinding(st); call != nil {
					if lines == nil {
						lines = strings.Split(source, "\n")
					}
					call.Gate = &ast.GateDoc{Name: name, Doc: docAbove(lines, st.Pos.Line, name)}
				}
			}
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		}
	}
	walk(reflect.ValueOf(prog))
}

// gateBinding is the name st binds a typed gate's result to and the gate's
// call — nil when st doesn't bind one.
func gateBinding(st *ast.Statement) (string, *ast.Call) {
	var name string
	var value *ast.Expr
	switch {
	case st.Var != nil:
		name, value = st.Var.Name, st.Var.Value
	case st.Const != nil:
		name, value = st.Const.Name, st.Const.Value
	case st.Assign != nil && st.Assign.Op == "=" && st.Assign.Target != nil:
		t := st.Assign.Target
		switch {
		case len(t.Ops) == 0 && t.Primary != nil:
			name = t.Primary.Ident
		case len(t.Ops) > 0 && t.Ops[len(t.Ops)-1].Member != "":
			name = t.Ops[len(t.Ops)-1].Member
		}
		value = st.Assign.Value
	}
	if name == "" {
		return "", nil
	}
	pf := ast.BarePostfix(value)
	if pf == nil || pf.Primary == nil || pf.Primary.Ident != "pause" || len(pf.Ops) != 1 || pf.Ops[0].Call == nil {
		return "", nil
	}
	for _, a := range pf.Ops[0].Call.Args {
		if a.Name == "decision" {
			return name, pf.Ops[0].Call
		}
	}
	return "", nil
}
