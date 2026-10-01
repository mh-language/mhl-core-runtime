package lint

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// checkConstReassign mirrors the interpreter's `const` enforcement
// (execAssign / execStatementBody): within a scope, a name bound by `const`
// may not be the target of `=` or `+=`, and a `var` may not re-declare it.
// Re-running the `const` *declaration* (e.g. inside a loop body) is allowed
// and rebinds — only assignment is an error.
//
// Scope matches every other statement-level check in this package: pipeline
// step bodies and `tool` method blocks only. A `test`/`describe` body is
// deliberately not walked — that is where the `sample/syntax/*_rejected`
// examples deliberately trigger the runtime error inside a `try/catch`, and
// flagging it statically there would just be editor noise on a file whose
// whole point is to demonstrate the failure.
func checkConstReassign(file string, prog *ast.Program) []Finding {
	var findings []Finding
	// param is the typed signature param (`workflow X(req: T)`) of the
	// pipeline being checked, "" elsewhere; inputs are its declared inputs.
	// Both are seeded as consts: they're read-only (the runtime rebinds them
	// before every step, so a write would be silently undone), each with its
	// own wording.
	param := ""
	inputs := map[string]bool{}

	check := func(stmts []*ast.Statement, seed map[string]bool) {
		consts := map[string]bool{}
		for k := range seed {
			consts[k] = true
		}
		var walk func([]*ast.Statement)
		walk = func(ss []*ast.Statement) {
			for _, s := range ss {
				switch {
				case s.Const != nil:
					consts[s.Const.Name] = true
				case s.Var != nil:
					if consts[s.Var.Name] {
						msg := fmt.Sprintf("%q is already declared as a constant", s.Var.Name)
						if s.Var.Name == param {
							msg = fmt.Sprintf("%q is the typed parameter and can't be redeclared", s.Var.Name)
						} else if inputs[s.Var.Name] {
							msg = fmt.Sprintf("%q is an input and can't be redeclared", s.Var.Name)
						}
						findings = append(findings, Finding{File: file, Line: s.Pos.Line, Column: s.Pos.Column, Message: msg})
					}
				case s.Assign != nil:
					if name, _, ok := assignTargetBase(s.Assign.Target); ok && consts[name] {
						msg := fmt.Sprintf("cannot assign to constant %q", name)
						if name == param {
							msg = fmt.Sprintf("cannot assign to %q: the typed parameter is read-only (copy it into a var to change it)", name)
						} else if inputs[name] {
							msg = fmt.Sprintf("cannot assign to input %q: inputs are read-only (copy it into a var to change it)", name)
						}
						findings = append(findings, Finding{File: file, Line: s.Pos.Line, Column: s.Pos.Column, Message: msg})
					}
				case s.If != nil:
					walk(s.If.Then)
					walk(s.If.Else)
				case s.While != nil:
					walk(s.While.Body)
				case s.ForIn != nil:
					walk(s.ForIn.Body)
				case s.Try != nil:
					walk(s.Try.Body)
					walk(s.Try.Catch)
					walk(s.Try.Finally)
				}
			}
		}
		walk(stmts)
	}

	for _, decl := range prog.Decls {
		switch {
		case decl.Pipeline != nil:
			pipelineConsts := map[string]bool{}
			inputs = map[string]bool{}
			for _, m := range decl.Pipeline.Body {
				if m.Const != nil {
					pipelineConsts[m.Const.Name] = true
				}
				if m.Input != nil {
					pipelineConsts[m.Input.Name] = true
					inputs[m.Input.Name] = true
				}
			}
			param = ""
			if decl.Pipeline.Param != nil {
				param = decl.Pipeline.Param.Name
				pipelineConsts[param] = true
			}
			for _, m := range decl.Pipeline.Body {
				var steps []*ast.Step
				switch {
				case m.Step != nil:
					steps = []*ast.Step{m.Step}
				case m.Parallel != nil:
					steps = m.Parallel.Steps
				default:
					continue
				}
				for _, step := range steps {
					check(step.Body, pipelineConsts)
				}
			}
		case decl.Tool != nil:
			param = ""
			inputs = map[string]bool{}
			toolConsts := map[string]bool{}
			for _, member := range decl.Tool.Members {
				if member.Const != nil {
					toolConsts[member.Const.Name] = true
				}
			}
			for _, m := range decl.Tool.Methods {
				visibleConsts := map[string]bool{}
				for name := range toolConsts {
					visibleConsts[name] = true
				}
				for _, param := range m.Params {
					delete(visibleConsts, param.Name)
				}
				check(m.Block, visibleConsts)
			}
		}
	}
	return findings
}
