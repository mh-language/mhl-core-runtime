package runtime

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/engine/value"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// pipelineFromAlias projects a workflow alias (ast.PipelineAlias) onto the
// Pipeline its target declares: same steps, vars, hooks and output, run
// under the alias's own Name (sessions, checkpoints, MCP tool) with Decl
// pointing at the target, the alias's description when it has one, and its
// bound inputs moved out of Inputs into Bound.
func pipelineFromAlias(a *ast.PipelineAlias, aliases map[string]types.Type, prog *ast.Program) (Pipeline, error) {
	target, err := AliasTarget(prog, a)
	if err != nil {
		return Pipeline{}, err
	}
	if target.Partial {
		if n := target.EntryStepCount(); n != 1 {
			return Pipeline{}, fmt.Errorf("%s %q: its target, partial %s %q, has %d `entry step`s across its fragments, want exactly one", a.Kind, a.Name, target.Kind, target.Name, n)
		}
	}
	p := PipelineFromAST(target, aliases, prog)
	p.Name = a.Name
	if d := a.Description(); d != "" {
		p.Description = d
	}
	if a.Bound == nil {
		return p, nil
	}
	p.Bound = map[string]any{}
	for _, f := range a.Bound.Fields {
		key := ""
		if f.KeyIdent != nil {
			key = *f.KeyIdent
		} else if f.KeyStr != nil {
			key = *f.KeyStr
		}
		idx := -1
		for i, in := range p.Inputs {
			if in.Name == key {
				idx = i
			}
		}
		if idx < 0 {
			return Pipeline{}, fmt.Errorf("%s %q: %s %q has no input %q to bind", a.Kind, a.Name, target.Kind, target.Name, key)
		}
		if _, dup := p.Bound[key]; dup {
			return Pipeline{}, fmt.Errorf("%s %q: input %q is bound twice", a.Kind, a.Name, key)
		}
		label := fmt.Sprintf("%s %q: bound input %q", a.Kind, a.Name, key)
		v, ok := staticValue(f.Value, prog)
		if !ok {
			return Pipeline{}, fmt.Errorf("%s must be a literal or an enum value", label)
		}
		in := p.Inputs[idx]
		if v, err = ConvertEnums(label, in.Type, v, p.Enums); err != nil {
			return Pipeline{}, err
		}
		if err := types.Check(label, in.Type, v); err != nil {
			return Pipeline{}, err
		}
		p.Bound[key] = v
		p.Inputs = append(p.Inputs[:idx:idx], p.Inputs[idx+1:]...)
	}
	return p, nil
}

// AliasTarget resolves the pipeline/workflow declaration a workflow alias
// points at. The target must be a real declaration of the same kind — not
// another alias.
func AliasTarget(prog *ast.Program, a *ast.PipelineAlias) (*ast.Pipeline, error) {
	name := a.Target
	if real, ok := prog.AliasMap()[name]; ok {
		name = real
	}
	for _, d := range prog.Decls {
		if d.Alias != nil && d.Alias.Name == name {
			return nil, fmt.Errorf("%s %q: target %q is itself an alias — point at the declaration it aliases", a.Kind, a.Name, a.Target)
		}
		if d.Pipeline != nil && d.Pipeline.Name == name {
			if d.Pipeline.Kind != a.Kind {
				return nil, fmt.Errorf("%s %q: target %q is a %s — an alias keeps its target's kind", a.Kind, a.Name, a.Target, d.Pipeline.Kind)
			}
			return d.Pipeline, nil
		}
	}
	return nil, fmt.Errorf("%s %q: target %q is not a declared pipeline or workflow", a.Kind, a.Name, a.Target)
}

// staticValue reads a bound input's value: a literal (string, number, bool,
// null), an enum value (`Level.delivery`), or an array/object of those.
func staticValue(e *ast.Expr, prog *ast.Program) (any, bool) {
	if pf := ast.BarePostfix(e); pf != nil && pf.Primary != nil {
		p := pf.Primary
		switch {
		case p.Ident != "" && len(pf.Ops) == 1 && pf.Ops[0].Member != "" && !pf.Ops[0].Optional:
			return enumConstant(prog, p.Ident, pf.Ops[0].Member)
		case len(pf.Ops) == 0 && p.Array != nil:
			out := make([]any, len(p.Array.Items))
			for i, item := range p.Array.Items {
				v, ok := staticValue(item, prog)
				if !ok {
					return nil, false
				}
				out[i] = v
			}
			return out, true
		case len(pf.Ops) == 0 && p.Object != nil:
			out := make(map[string]any, len(p.Object.Fields))
			for _, f := range p.Object.Fields {
				key := ""
				if f.KeyIdent != nil {
					key = *f.KeyIdent
				} else if f.KeyStr != nil {
					key = *f.KeyStr
				}
				v, ok := staticValue(f.Value, prog)
				if !ok {
					return nil, false
				}
				out[key] = v
			}
			return out, true
		}
	}
	return ast.LiteralValue(e)
}

func enumConstant(prog *ast.Program, name, variant string) (any, bool) {
	if real, ok := prog.AliasMap()[name]; ok {
		name = real
	}
	for _, d := range prog.Decls {
		if d.Enum != nil && d.Enum.Name == name {
			for _, v := range d.Enum.Variants {
				if v == variant {
					return value.Enum{Enum: name, Variant: variant}, true
				}
			}
			return nil, false
		}
	}
	return nil, false
}
