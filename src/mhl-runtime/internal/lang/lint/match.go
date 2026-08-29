package lint

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// checkMatchInExpr validates every `match` expression reachable from expr:
//   - a duplicate pattern, or an arm after the `_` wildcard, is always an error;
//   - a `match` whose subject is statically known to be an `enum` or a `bool`,
//     with no `_` arm, must cover every case — a missing one is an error.
//
// A subject of any other kind can't be proven exhaustive, so it is left to
// the runtime "no arm matched" error.
func checkMatchInExpr(file string, prog *ast.Program, expr *ast.Expr, declared map[string]types.Type, aliases map[string]types.Type) []Finding {
	var findings []Finding
	walkMatchExprs(expr, func(m *ast.MatchExpr) {
		findings = append(findings, checkOneMatch(file, prog, m, declared, aliases)...)
	})
	return findings
}

func checkOneMatch(file string, prog *ast.Program, m *ast.MatchExpr, declared map[string]types.Type, aliases map[string]types.Type) []Finding {
	var findings []Finding

	seen := map[string]bool{}
	wildcardAt := -1
	for i, arm := range m.Arms {
		if arm.Wildcard {
			if wildcardAt == -1 {
				wildcardAt = i
			}
			continue
		}
		if wildcardAt != -1 {
			findings = append(findings, Finding{File: file, Line: arm.Pos.Line, Column: arm.Pos.Column,
				Message: "match arm is unreachable: it follows the `_` wildcard"})
			continue
		}
		key := patternKey(arm.Pattern)
		if key != "" && seen[key] {
			findings = append(findings, Finding{File: file, Line: arm.Pos.Line, Column: arm.Pos.Column,
				Message: fmt.Sprintf("duplicate match pattern %s", key)})
		}
		seen[key] = true
	}
	hasWildcard := wildcardAt != -1

	if enumName, ok := matchSubjectEnum(prog, m.Subject, declared, aliases); ok && !hasWildcard {
		e, _ := findEnumDecl(prog, enumName)
		covered := map[string]bool{}
		for _, arm := range m.Arms {
			if arm.Wildcard {
				continue
			}
			if en, v, ok := patternEnumVariant(arm.Pattern); ok && en == enumName {
				covered[v] = true
			}
		}
		var missing []string
		for _, v := range e.Variants {
			if !covered[v] {
				missing = append(missing, v)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			findings = append(findings, Finding{File: file, Line: m.Pos.Line, Column: m.Pos.Column,
				Message: fmt.Sprintf("match on enum %q is not exhaustive: missing %s", enumName, strings.Join(missing, ", "))})
		}
		return findings
	}

	if matchSubjectBool(m.Subject, declared) && !hasWildcard {
		covered := map[string]bool{}
		for _, arm := range m.Arms {
			if arm.Wildcard {
				continue
			}
			if b, ok := ast.BoolValue(arm.Pattern); ok {
				covered[fmt.Sprintf("%v", b)] = true
			}
		}
		var missing []string
		for _, b := range []string{"true", "false"} {
			if !covered[b] {
				missing = append(missing, b)
			}
		}
		if len(missing) > 0 {
			findings = append(findings, Finding{File: file, Line: m.Pos.Line, Column: m.Pos.Column,
				Message: fmt.Sprintf("match on bool is not exhaustive: missing %s", strings.Join(missing, ", "))})
		}
	}
	return findings
}

// patternKey renders a stable identity for a match pattern, for
// duplicate detection. "" means "can't summarise" (skip the check).
func patternKey(e *ast.Expr) string {
	if en, v, ok := patternEnumVariant(e); ok {
		return en + "." + v
	}
	if lv, err := literalValue(e); err == nil {
		return fmt.Sprintf("%T:%v", lv, lv)
	}
	return ""
}

// patternEnumVariant recognises an `Enum.Variant` pattern (a bare ident
// with exactly one plain member trailer).
func patternEnumVariant(e *ast.Expr) (enumName, variant string, ok bool) {
	p := ast.BarePostfix(e)
	if p == nil || p.Primary == nil || p.Primary.Ident == "" || len(p.Ops) != 1 {
		return "", "", false
	}
	op := p.Ops[0]
	if op.Member == "" || op.Optional || op.Call != nil || op.Index != nil || op.Slice != nil || op.OptIndex != nil {
		return "", "", false
	}
	return p.Primary.Ident, op.Member, true
}

// matchSubjectEnum reports the declared enum name a match subject belongs
// to, when that is statically knowable: the subject is `Enum.Variant`, or a
// bare identifier whose known type (param/input/inferred var, alias
// included) is that enum.
func matchSubjectEnum(prog *ast.Program, subject *ast.Expr, declared map[string]types.Type, aliases map[string]types.Type) (string, bool) {
	if en, _, ok := patternEnumVariant(subject); ok {
		if _, isEnum := findEnumDecl(prog, en); isEnum {
			return en, true
		}
	}
	if name, ok := ast.IdentValue(subject); ok {
		if t, ok := declared[name]; ok && t.Kind == types.EnumKind {
			return t.Name, true
		}
	}
	return "", false
}

func matchSubjectBool(subject *ast.Expr, declared map[string]types.Type) bool {
	if _, ok := ast.BoolValue(subject); ok {
		return true
	}
	if name, ok := ast.IdentValue(subject); ok {
		if t, ok := declared[name]; ok && t.Kind == types.BoolKind {
			return true
		}
	}
	return false
}

// findEnumDecl is lint's own enum lookup (the interpreter has its own copy).
func findEnumDecl(prog *ast.Program, name string) (*ast.Enum, bool) {
	for _, decl := range prog.Decls {
		if decl.Enum != nil && decl.Enum.Name == name {
			return decl.Enum, true
		}
	}
	return nil, false
}
