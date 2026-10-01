package lint

import (
	"fmt"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// checkPipelineAliases statically mirrors runtime.pipelineFromAlias for every
// workflow alias (`workflow Delivery = ArtifactFlow with { level: ... }`):
// the target is a declared pipeline/workflow of the same kind (not another
// alias); every bound key is one of the target's inputs (or a field of its
// typed parameter), bound once, to a literal or an enum value of the input's
// type; and the alias's own body sets nothing but `description`.
func checkPipelineAliases(file string, prog *ast.Program, aliases map[string]types.Type) []Finding {
	var findings []Finding
	add := func(pos lexer.Position, format string, args ...any) {
		findings = append(findings, Finding{File: file, Line: pos.Line, Column: pos.Column, Message: fmt.Sprintf(format, args...)})
	}
	for _, decl := range prog.Decls {
		a := decl.Alias
		if a == nil {
			continue
		}
		label := fmt.Sprintf("%s %q", a.Kind, a.Name)
		for _, p := range a.Props {
			if p.Name != "description" {
				add(p.Pos, "%s: an alias may only set `description`, not %q — everything else comes from %q", label, p.Name, a.Target)
			} else if _, ok := ast.StringValue(p.Value); !ok {
				add(p.Pos, "%s: description must be a string literal", label)
			}
		}
		target, msg := aliasTarget(prog, a)
		if target == nil {
			add(a.Pos, "%s: %s", label, msg)
			continue
		}
		inputs := targetInputTypes(target, aliases)
		if inputs == nil || a.Bound == nil {
			continue
		}
		seen := map[string]bool{}
		for _, f := range a.Bound.Fields {
			key := ""
			if f.KeyIdent != nil {
				key = *f.KeyIdent
			} else if f.KeyStr != nil {
				key = *f.KeyStr
			}
			pos := a.Pos
			if f.Value != nil {
				pos = f.Value.Pos
			}
			t, ok := inputs[key]
			if !ok {
				add(pos, "%s: %s %q has no input %q to bind", label, target.Kind, target.Name, key)
				continue
			}
			if seen[key] {
				add(pos, "%s: input %q is bound twice", label, key)
				continue
			}
			seen[key] = true
			if msg := checkBoundValue(prog, t, f.Value); msg != "" {
				add(pos, "%s: bound input %q %s", label, key, msg)
			}
		}
	}
	return findings
}

func aliasTarget(prog *ast.Program, a *ast.PipelineAlias) (*ast.Pipeline, string) {
	name := resolveName(prog, a.Target)
	for _, d := range prog.Decls {
		if d.Alias != nil && d.Alias.Name == name {
			return nil, fmt.Sprintf("target %q is itself an alias — point at the declaration it aliases", a.Target)
		}
		if d.Pipeline != nil && d.Pipeline.Name == name {
			if d.Pipeline.Kind != a.Kind {
				return nil, fmt.Sprintf("target %q is a %s — an alias keeps its target's kind", a.Target, d.Pipeline.Kind)
			}
			return d.Pipeline, ""
		}
	}
	return nil, fmt.Sprintf("target %q is not a declared pipeline or workflow", a.Target)
}

// targetInputTypes is the target's bindable inputs: its `input` members, or
// its typed parameter's fields. nil when the parameter type is unresolved
// (checkPipelineSignature reports that).
func targetInputTypes(p *ast.Pipeline, aliases map[string]types.Type) map[string]types.Type {
	if p.Param != nil {
		t, ok := types.FromExprAlias(p.Param.Type, aliases)
		if !ok || t.Fields == nil {
			return nil
		}
		return t.Fields
	}
	return pipelineInputTypes(p, aliases)
}

// checkBoundValue returns "" when e is a literal or enum value of type t,
// else the reason it is not.
func checkBoundValue(prog *ast.Program, t types.Type, e *ast.Expr) string {
	if pf := ast.BarePostfix(e); pf != nil && pf.Primary != nil && pf.Primary.Ident != "" &&
		len(pf.Ops) == 1 && pf.Ops[0].Member != "" && !pf.Ops[0].Optional {
		enum, ok := findEnumDecl(prog, resolveName(prog, pf.Primary.Ident))
		if !ok {
			return "must be a literal or an enum value"
		}
		if !enumHasVariantName(enum, pf.Ops[0].Member) {
			return fmt.Sprintf("names no variant of enum %s (use %s)", enum.Name, strings.Join(enum.Variants, " | "))
		}
		if t.Kind != types.EnumKind || t.Name != enum.Name {
			return fmt.Sprintf("must be %s, got enum %s", t, enum.Name)
		}
		return ""
	}
	v, ok := ast.LiteralValue(e)
	if !ok {
		return "must be a literal or an enum value"
	}
	if s, isStr := v.(string); isStr && t.Kind == types.EnumKind {
		if enum, ok := findEnumDecl(prog, t.Name); ok && !enumHasVariantName(enum, s) {
			return fmt.Sprintf("%q is not a variant of enum %s (use %s)", s, enum.Name, strings.Join(enum.Variants, " | "))
		}
		return ""
	}
	if err := types.Check("value", t, v); err != nil {
		return strings.TrimPrefix(err.Error(), "value ")
	}
	return ""
}

func enumHasVariantName(e *ast.Enum, name string) bool {
	for _, v := range e.Variants {
		if v == name {
			return true
		}
	}
	return false
}
