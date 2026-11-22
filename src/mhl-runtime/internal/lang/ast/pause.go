package ast

import "reflect"

var postfixType = reflect.TypeOf(Postfix{})

// CallsPause reports whether body calls the `pause(...)` builtin anywhere —
// a plain pause or a typed gate (`pause(reason, decision: T)`), at any depth.
// Static: a pause behind a condition counts, since the run may reach it.
func CallsPause(body []*Statement) bool {
	found := false
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		if found {
			return
		}
		switch v.Kind() {
		case reflect.Ptr, reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Slice:
			for i := 0; i < v.Len() && !found; i++ {
				walk(v.Index(i))
			}
		case reflect.Struct:
			if v.Type() == postfixType {
				pf := v.Addr().Interface().(*Postfix)
				if pf.Primary != nil && pf.Primary.Ident == "pause" && len(pf.Ops) > 0 && pf.Ops[0].Call != nil {
					found = true
					return
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
	return found
}
