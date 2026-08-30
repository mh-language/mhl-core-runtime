package lint

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// pipelineInputs returns every `input name: Type` declaration in p — the
// single walk both checkPipelineInputTypes and pipelineInputTypes (below)
// build on, instead of each re-scanning p.Body on its own.
func pipelineInputs(p *ast.Pipeline) []*ast.PipelineInput {
	var inputs []*ast.PipelineInput
	for _, member := range p.Body {
		if member.Input != nil {
			inputs = append(inputs, member.Input)
		}
	}
	return inputs
}

// pipelineInputTypes maps each declared input to its parsed Type, falling
// back to types.Any for one whose Type text doesn't resolve — reporting
// that typo is checkPipelineInputTypes' job, not this reader's; variable-
// type inference (varinfer.go) simply treats an unresolvable input as
// unknown, the same as anywhere else.
func pipelineInputTypes(p *ast.Pipeline, aliases map[string]types.Type) map[string]types.Type {
	m := map[string]types.Type{}
	for _, in := range pipelineInputs(p) {
		t, ok := types.FromExprAlias(in.Type, aliases)
		if !ok {
			t = types.Any
		}
		m[in.Name] = t
	}
	return m
}

// toolMethodParamTypes maps a tool method's declared parameters to their
// resolved types (alias- and enum-aware), for seeding a scope's known-type
// map. An untyped or unresolvable parameter maps to types.Any. A fresh map
// per call — safe to hand to collectVarNames as a seed.
func toolMethodParamTypes(m *ast.ToolMethod, aliases map[string]types.Type) map[string]types.Type {
	out := make(map[string]types.Type, len(m.Params))
	for _, p := range m.Params {
		t, ok := types.FromExprAlias(p.Type, aliases)
		if !ok {
			t = types.Any
		}
		out[p.Name] = t
	}
	return out
}

// checkPipelineInputTypes reports every `input name: Type` declaration whose
// Type text doesn't resolve via types.Parse (an unrecognized keyword, most
// often a typo). This is the only static check possible for pipeline
// inputs today: PipelineInput has no default-value expression to type-check
// against, and --input values are CLI arguments invisible to lint — the
// actual coerce-or-fail-fast behavior against a declared type happens at
// `mhl run` time in internal/cli's runPipeline.
func checkPipelineInputTypes(file string, prog *ast.Program, aliases map[string]types.Type) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		if decl.Pipeline == nil {
			continue
		}
		for _, in := range pipelineInputs(decl.Pipeline) {
			if _, ok := types.FromExprAlias(in.Type, aliases); !ok {
				findings = append(findings, Finding{
					File:    file,
					Line:    in.Pos.Line,
					Column:  in.Pos.Column,
					Message: fmt.Sprintf("input %q: unrecognized type %q", in.Name, in.Type),
				})
			}
		}
	}
	return findings
}

// checkToolMethodReturnTypes reports two things for every `): Type ->`
// return-type annotation: an unrecognized Type keyword (a typo), and any
// return value the method's own declaration proves is the wrong type — a
// single-expression Body, or a literal `return <expr>` inside a Block
// (recursing into if/while/try, same traversal collectVarNames already
// uses). A Block's non-literal return (a variable, a call) can't be proven
// statically here — same "can't prove it, don't fail" stance the rest of
// lint already takes — and is left to evalToolCall's runtime check.
func checkToolMethodReturnTypes(file string, prog *ast.Program, aliases map[string]types.Type) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		if decl.Tool == nil {
			continue
		}
		for _, m := range decl.Tool.Methods {
			if m.Returns == nil {
				continue
			}
			declared, ok := types.FromExprAlias(m.Returns, aliases)
			if !ok {
				findings = append(findings, Finding{
					File:    file,
					Line:    m.Pos.Line,
					Column:  m.Pos.Column,
					Message: fmt.Sprintf("tool %q: %s: unrecognized return type %q", decl.Tool.Name, m.Name, m.Returns),
				})
				continue
			}
			for _, retExpr := range toolMethodReturnExprs(m) {
				lv, err := literalValue(retExpr)
				if err != nil {
					continue
				}
				if err := types.Check(fmt.Sprintf("tool %q: %s: return value", decl.Tool.Name, m.Name), declared, lv); err != nil {
					findings = append(findings, Finding{File: file, Line: m.Pos.Line, Column: m.Pos.Column, Message: err.Error()})
				}
			}
		}
	}
	return findings
}

// toolMethodReturnExprs collects every expression a ToolMethod may return: a
// single-expression Body, or every `return <expr>` inside a Block. A bare
// `return` with no value, or a Block that falls off its end with no return
// at all, both evaluate to nil at runtime — nil satisfies any declared type
// (types.Check), so those aren't collected here.
func toolMethodReturnExprs(m *ast.ToolMethod) []*ast.Expr {
	if m.Body != nil {
		return []*ast.Expr{m.Body}
	}
	var exprs []*ast.Expr
	var walk func([]*ast.Statement)
	walk = func(stmts []*ast.Statement) {
		for _, s := range stmts {
			switch {
			case s.Return != nil && s.Return.Value != nil:
				exprs = append(exprs, s.Return.Value)
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
	walk(m.Block)
	return exprs
}
