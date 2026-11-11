package lint

import (
	"reflect"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// checkRunCallArguments holds every `Agent.run(...)` and `Router.delegate(...)`
// call in the program to the arguments the runtime accepts (unknownRunArg:
// named only, a run argument or a declared input, every required input
// present) — wherever the call is written: a workflow step, a tool method
// (block or single-expression body), a test, a lambda, a hook, an agent's
// own before/after, or nested inside another expression
// (`json.parse(Coder.run(...))`). checkAgentCall only ever saw a bare call
// statement inside a pipeline step, so the same typo passed silently from a
// tool method — where a host calling agents through a `tool` keeps all of
// them.
//
// It walks the AST by reflection, like parser.checkSpreadPlacement, so a
// call site in a node added to the grammar later is covered too.
func checkRunCallArguments(file string, prog *ast.Program) []Finding {
	w := runCallWalker{file: file, prog: prog, seen: map[*ast.Call]bool{}}
	w.walk(reflect.ValueOf(prog), lexer.Position{})
	return w.findings
}

type runCallWalker struct {
	file     string
	prog     *ast.Program
	seen     map[*ast.Call]bool
	findings []Finding
}

var (
	lintExprType    = reflect.TypeOf(ast.Expr{})
	lintPostfixType = reflect.TypeOf(ast.Postfix{})
)

// walk visits v; pos is the position of the innermost enclosing Expr — a
// Postfix carries none of its own.
func (w *runCallWalker) walk(v reflect.Value, pos lexer.Position) {
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if !v.IsNil() {
			w.walk(v.Elem(), pos)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			w.walk(v.Index(i), pos)
		}
	case reflect.Struct:
		switch v.Type() {
		case lintExprType:
			pos = v.Addr().Interface().(*ast.Expr).Pos
		case lintPostfixType:
			w.postfix(v.Addr().Interface().(*ast.Postfix), pos)
		}
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).PkgPath == "" {
				w.walk(v.Field(i), pos)
			}
		}
	}
}

func (w *runCallWalker) postfix(p *ast.Postfix, pos lexer.Position) {
	if p.Primary == nil || p.Primary.Ident == "" || len(p.Ops) < 2 || p.Ops[1].Call == nil || p.Ops[0].Optional {
		return
	}
	call := p.Ops[1].Call
	if w.seen[call] {
		return
	}
	w.seen[call] = true
	name := resolveName(w.prog, p.Primary.Ident)
	var msg string
	var bad bool
	switch p.Ops[0].Member {
	case "run":
		agent, ok := findAgent(w.prog, name)
		if !ok {
			return // "agent not found" is checkAgentCall's / the undefined-name check's
		}
		msg, bad = unknownRunArg(p.Primary.Ident+".run", call, agent)
	case "delegate":
		router, ok := findRouter(w.prog, name)
		if !ok {
			return
		}
		msg, bad = unknownRunArg(p.Primary.Ident+".delegate", call, routerAgentDecls(w.prog, router)...)
	default:
		return
	}
	if bad {
		w.findings = append(w.findings, Finding{File: w.file, Line: pos.Line, Column: pos.Column, Message: msg})
	}
}
