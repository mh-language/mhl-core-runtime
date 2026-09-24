package lint

import (
	"fmt"
	"sort"

	"github.com/alecthomas/participle/v2/lexer"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// checkPipelineSignature validates a pipeline/workflow's optional typed
// signature — `workflow Review(req: ReviewInput): ReviewOutput { ... }`
// (ast.Pipeline.Param / Returns):
//
//   - the param type and the result type must resolve, and each must be a
//     shaped object type (`{ ... }` or an alias of one): the param's fields
//     are the inputs, and `output:` is always an object mapping;
//   - the param replaces per-line `input x: T` members — mixing both is an
//     error, as is a pipeline-level var/const/mem with the param's name
//     (the runtime binds the param over it every step);
//   - a result type requires an explicit `output: { ... }` projection — the
//     result is never projected implicitly from same-named vars;
//   - when `output:` is an object literal, its keys must be exactly the
//     result type's fields: every required field present, none undeclared.
//     A non-literal `output:` is only checked at run time
//     (execsvc's types.Check → runtime.OutputContractError).
func checkPipelineSignature(file string, prog *ast.Program, aliases map[string]types.Type) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		p := decl.Pipeline
		if p == nil || (p.Param == nil && p.Returns == nil) {
			continue
		}
		add := func(pos lexer.Position, msg string) {
			findings = append(findings, Finding{File: file, Line: pos.Line, Column: pos.Column, Message: msg})
		}
		label := fmt.Sprintf("%s %q", pipelineKind(p), p.Name)

		if p.Param != nil {
			if t, ok := types.FromExprAlias(p.Param.Type, aliases); !ok {
				add(p.Param.Pos, fmt.Sprintf("%s: parameter %q: unrecognized type %q", label, p.Param.Name, p.Param.Type))
			} else if t.Kind != types.ObjectKind || t.Fields == nil {
				add(p.Param.Pos, fmt.Sprintf("%s: parameter %q must be an object type (e.g. `type %sInput = { ... }`), got %s", label, p.Param.Name, p.Name, t))
			}
			for _, m := range p.Body {
				switch {
				case m.Input != nil:
					add(m.Input.Pos, fmt.Sprintf("%s: `input %s` can't be combined with the typed parameter %q — declare it as a field of %s instead", label, m.Input.Name, p.Param.Name, p.Param.Type))
				case m.Var != nil && m.Var.Name == p.Param.Name,
					m.Const != nil && m.Const.Name == p.Param.Name,
					m.Mem != nil && m.Mem.Name == p.Param.Name:
					add(p.Param.Pos, fmt.Sprintf("%s: a pipeline-level declaration shadows the typed parameter %q — rename one", label, p.Param.Name))
				}
			}
		}

		if p.Returns == nil {
			continue
		}
		rt, ok := types.FromExprAlias(p.Returns, aliases)
		if !ok {
			add(p.Pos, fmt.Sprintf("%s: unrecognized result type %q", label, p.Returns))
			continue
		}
		if rt.Kind != types.ObjectKind || rt.Fields == nil {
			add(p.Pos, fmt.Sprintf("%s: result type must be an object type (`output:` is always an object mapping), got %s", label, rt))
			continue
		}
		var output *ast.Property
		for _, m := range p.Body {
			if m.Prop != nil && m.Prop.Name == "output" {
				output = m.Prop
			}
		}
		if output == nil {
			add(p.Pos, fmt.Sprintf("%s declares result type %s but no `output: { ... }` projection — a typed result is always projected explicitly", label, p.Returns))
			continue
		}
		obj := ast.BareObject(output.Value)
		if obj == nil {
			continue
		}
		keys := map[string]bool{}
		for _, f := range obj.Fields {
			key := ""
			switch {
			case f.KeyIdent != nil:
				key = *f.KeyIdent
			case f.KeyStr != nil:
				key = *f.KeyStr
			}
			keys[key] = true
			if _, declared := rt.Fields[key]; !declared {
				add(output.Pos, fmt.Sprintf("%s: output field %q is not declared in %s", label, key, p.Returns))
			}
		}
		var missing []string
		for name := range rt.Fields {
			if !keys[name] && !rt.IsOptional(name) {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		for _, name := range missing {
			add(output.Pos, fmt.Sprintf("%s: output is missing field %q required by %s", label, name, p.Returns))
		}
	}
	return findings
}
