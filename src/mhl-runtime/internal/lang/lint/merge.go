package lint

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// checkParallelMerges checks each parallel group's `merge (param) { ... }`
// block (ast.ParallelMerge) — the rules runtime.runMerge relies on:
//
//   - `pause(...)` and `complete()` are not called in it (`goto`/`break` are
//     checkParallelGroups', with every other step of a group): the merge is
//     part of the group's barrier, not a step the run can stop or end at;
//   - param does not shadow a workflow input, var, const, mem or its typed
//     signature's param — runMerge binds it over that name for the merge;
//   - every `param.X` names one of the group's branch steps.
//
// Writes to param are checkConstReassign's (it is seeded as a const there).
func checkParallelMerges(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		p := decl.Pipeline
		if p == nil {
			continue
		}
		taken := map[string]string{}
		for _, m := range p.Body {
			switch {
			case m.Input != nil:
				taken[m.Input.Name] = "input"
			case m.Var != nil:
				taken[m.Var.Name] = "var"
			case m.Const != nil:
				taken[m.Const.Name] = "const"
			case m.Mem != nil:
				taken[m.Mem.Name] = "mem"
			}
		}
		if p.Param != nil {
			taken[p.Param.Name] = "parameter"
		}
		for _, m := range p.Body {
			pg := m.Parallel
			if pg == nil || pg.Merge == nil {
				continue
			}
			mg := pg.Merge
			at := func(pos lexer.Position, msg string) {
				findings = append(findings, Finding{File: fileAt(file, pos), Line: pos.Line, Column: pos.Column, Message: msg})
			}
			if kind, ok := taken[mg.Param]; ok {
				at(mg.Pos, fmt.Sprintf("merge of parallel group %q: parameter %q shadows the %s %q of %s %q — name it something else (e.g. branches)", pg.Name, mg.Param, kind, mg.Param, p.Kind, p.Name))
			}
			branches := map[string]bool{}
			names := make([]string, 0, len(pg.Steps))
			for _, s := range pg.Steps {
				branches[s.Name] = true
				names = append(names, s.Name)
			}
			sort.Strings(names)
			walkPostfixes(mg.Body, mg.Pos, func(pf *ast.Postfix, pos lexer.Position) {
				id := pf.Primary.Ident
				switch {
				case (id == "pause" || id == "complete") && len(pf.Ops) > 0 && pf.Ops[0].Call != nil:
					at(pos, fmt.Sprintf("`%s()` cannot be used inside the merge of parallel group %q", id, pg.Name))
				case id == mg.Param && len(pf.Ops) > 0 && pf.Ops[0].Member != "" && !branches[pf.Ops[0].Member]:
					at(pos, fmt.Sprintf("%s.%s: parallel group %q has no step %q (its steps: %s)", id, pf.Ops[0].Member, pg.Name, pf.Ops[0].Member, strings.Join(names, ", ")))
				}
			})
		}
	}
	return findings
}

// mergeParam is the branches parameter in scope in step, when step is m's
// merge (a parallel group's ast.MergeStep), else "".
func mergeParam(m *ast.PipelineMember, step *ast.Step) string {
	if m.Parallel == nil || m.Parallel.Merge == nil || step.Name != ast.MergeStepName(m.Parallel.Name) {
		return ""
	}
	return m.Parallel.Merge.Param
}

// valueReturningMethods are the value methods (interpreter.callValueMethod)
// whose name reads like an in-place change but which return a new value and
// leave the receiver as it was — every value method does, since values are
// never aliased. Lambda-taking ones (map, filter, ...) are left out: a call
// for its lambda's side effects is odd, but not a no-op.
var valueReturningMethods = map[string]string{
	"append": "array", "remove": "value", "remove_content": "string",
	"replace": "string", "trim": "string", "to_upper": "string", "to_lower": "string",
	"unique": "array", "substring": "string", "join": "string",
}

// checkDiscardedResults flags a statement that is only a value method call
// on a variable — `self.reply.append(x)`, `name.trim()` — whose result goes
// nowhere: values are never aliased, so the call changes nothing and the
// line is a silent no-op. The fix is to assign the result. A receiver that
// is a declaration (a tool, memory, extension, ...) or a native namespace is
// not a value, so its methods are left alone.
func checkDiscardedResults(file string, prog *ast.Program) []Finding {
	var findings []Finding
	walkStatements(reflect.ValueOf(prog), func(s *ast.Statement) {
		if s.Expr == nil {
			return
		}
		pf := ast.BarePostfix(s.Expr.Expr)
		if pf == nil || pf.Primary == nil || pf.Primary.Ident == "" || len(pf.WithTail) > 0 || len(pf.Ops) < 2 {
			return
		}
		root := pf.Primary.Ident
		if root != "self" && (nativeNamespaces[root] || receiverBuiltinNames[root] || declaredNameExists(prog, root)) {
			return
		}
		last, method := pf.Ops[len(pf.Ops)-1], pf.Ops[len(pf.Ops)-2]
		kind, ok := valueReturningMethods[method.Member]
		if last.Call == nil || method.Member == "" || method.Optional || !ok {
			return
		}
		// A call on a member of a declaration reached through self (a tool
		// method's `self.client.remove(...)`) still has a value receiver; only
		// the root decides.
		recv := receiverText(pf.Primary.Ident, pf.Ops[:len(pf.Ops)-2])
		if recv == "" {
			return
		}
		findings = append(findings, Finding{File: fileAt(file, s.Pos), Line: s.Pos.Line, Column: s.Pos.Column,
			Message: fmt.Sprintf("the result of %s.%s(...) is discarded: %s() returns a new %s and leaves %s unchanged — assign it (%s = %s.%s(...))",
				recv, method.Member, method.Member, kind, recv, recv, recv, method.Member)})
	})
	return findings
}

// receiverText spells a receiver chain of plain member accesses
// (`self.reply`), "" when it has an index, a call or an optional step.
func receiverText(root string, ops []*ast.Trailer) string {
	var b strings.Builder
	b.WriteString(root)
	for _, op := range ops {
		if op.Member == "" || op.Optional {
			return ""
		}
		b.WriteString("." + op.Member)
	}
	return b.String()
}

// walkStatements calls fn for every ast.Statement reachable from v (steps,
// tool method blocks, hook lambdas, tests, nested blocks), by reflection.
func walkStatements(v reflect.Value, fn func(*ast.Statement)) {
	stmt := reflect.TypeOf(ast.Statement{})
	seen := map[uintptr]bool{}
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Ptr:
			if v.IsNil() || seen[v.Pointer()] {
				return
			}
			seen[v.Pointer()] = true
			walk(v.Elem())
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Struct:
			if v.Type() == stmt && v.CanAddr() {
				fn(v.Addr().Interface().(*ast.Statement))
			}
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		}
	}
	walk(v)
}

// walkPostfixes calls fn for every ast.Postfix reachable from body, with the
// position of the nearest enclosing node that carries one.
func walkPostfixes(body []*ast.Statement, start lexer.Position, fn func(*ast.Postfix, lexer.Position)) {
	postfix := reflect.TypeOf(ast.Postfix{})
	posType := reflect.TypeOf(lexer.Position{})
	var walk func(v reflect.Value, pos lexer.Position)
	walk = func(v reflect.Value, pos lexer.Position) {
		switch v.Kind() {
		case reflect.Ptr, reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem(), pos)
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), pos)
			}
		case reflect.Struct:
			if f := v.FieldByName("Pos"); f.IsValid() && f.Type() == posType {
				if p := f.Interface().(lexer.Position); p.Line > 0 {
					pos = p
				}
			}
			if v.Type() == postfix && v.CanAddr() {
				fn(v.Addr().Interface().(*ast.Postfix), pos)
			}
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i), pos)
				}
			}
		}
	}
	walk(reflect.ValueOf(body), start)
}
