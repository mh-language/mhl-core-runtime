package parser

import (
	"github.com/alecthomas/participle/v2"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// normalizePromptVariants turns each variant prompt body (`{ default ...
// for "p" ... }`) into its canonical shape: the single, mandatory `default`
// arm's body moves into Prompt.Body/Source — so every existing code path
// (rendering, lint's static rendering, flow's Detail, `.frontmatter`) sees
// the default exactly as it would a plain prompt, and a body that declares
// only a default hashes like the plain form — and Prompt.Variants keeps just
// the `for` arms (nil when there are none).
//
// It runs in Parse rather than lint because `mhl run` never lints: a prompt
// with no default, two defaults, a malformed pattern or a duplicate pattern
// must fail every entry point the same way.
func normalizePromptVariants(prog *ast.Program) error {
	for _, decl := range prog.Decls {
		p := decl.Prompt
		if p == nil || len(p.Variants) == 0 {
			continue
		}
		var def *ast.PromptVariant
		arms := make([]*ast.PromptVariant, 0, len(p.Variants))
		seen := map[string]bool{}
		for _, v := range p.Variants {
			if v.Default {
				if def != nil {
					return participle.Errorf(v.Pos, "prompt %q declares the default arm twice", p.Name)
				}
				def = v
				continue
			}
			if err := ast.ValidPromptPattern(v.Pattern); err != nil {
				return participle.Errorf(v.Pos, "prompt %q: %v", p.Name, err)
			}
			if seen[v.Pattern] {
				return participle.Errorf(v.Pos, "prompt %q declares the variant for %q twice", p.Name, v.Pattern)
			}
			seen[v.Pattern] = true
			arms = append(arms, v)
		}
		if def == nil {
			return participle.Errorf(p.Pos, "prompt %q declares `for` variants but no default arm", p.Name)
		}
		p.Body, p.Source = def.Body, def.Source
		if len(arms) == 0 {
			arms = nil
		}
		p.Variants = arms
	}
	return nil
}
