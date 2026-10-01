package interpreter

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// resolvePartials collapses every `partial pipeline`/`partial workflow`
// fragment sharing a Name in prog.Decls into one ast.Pipeline with one
// concatenated Body, replacing the group's declarations with a single merged
// one at the position of the first fragment found. That first-position
// choice is what keeps runtime.FindPipeline(prog, "") — "the first pipeline
// declared" — picking the entry file's own declaration afterward: a
// whole-file `import "..."` only ever appends a sibling's declarations after
// the importer's own (see resolveImports), so the entry file's fragment is
// always first in prog.Decls before this runs.
//
// A pipeline never marked `partial` is untouched — this only ever looks at
// decl.Pipeline.Partial. Mirrored, not shared, by lint.mergePartials: see
// that function's doc comment for why the two stay separate implementations,
// the same way ResolveImports and lint.mergeImports already do. The two
// deliberately diverge on one thing: lint.mergePartials reports a merged
// group's entry-step count as a Finding (so `mhl lint` says something about
// a lone fragment without failing the file); this function doesn't check it
// at all — see mergePartialGroup's doc comment and runtime.FindPipeline for
// where and why that check actually happens.
func resolvePartials(prog *ast.Program) error {
	type group struct {
		firstIdx int
		kind     string
		frags    []*ast.Pipeline
	}
	groups := map[string]*group{}
	var order []string
	for i, decl := range prog.Decls {
		if decl.Pipeline == nil || !decl.Pipeline.Partial {
			continue
		}
		p := decl.Pipeline
		g, ok := groups[p.Name]
		if !ok {
			g = &group{firstIdx: i, kind: p.Kind}
			groups[p.Name] = g
			order = append(order, p.Name)
		} else if p.Kind != g.kind {
			return fmt.Errorf("partial %s %q: one fragment declares it %q, another %q", g.kind, p.Name, g.kind, p.Kind)
		}
		g.frags = append(g.frags, p)
	}

	merged := make(map[string]*ast.Pipeline, len(order))
	for _, name := range order {
		m, err := mergePartialGroup(name, groups[name].kind, groups[name].frags)
		if err != nil {
			return err
		}
		merged[name] = m
	}

	out := make([]*ast.Declaration, 0, len(prog.Decls))
	for i, decl := range prog.Decls {
		if decl.Pipeline == nil || !decl.Pipeline.Partial {
			out = append(out, decl)
			continue
		}
		g := groups[decl.Pipeline.Name]
		if i != g.firstIdx {
			continue // a later fragment of an already-emitted group
		}
		out = append(out, &ast.Declaration{Export: decl.Export, Pipeline: merged[decl.Pipeline.Name]})
	}
	prog.Decls = out
	return nil
}

// mergePartialGroup combines frags (every fragment of one partial
// pipeline/workflow, in declaration order) into a single ast.Pipeline.
// Position is taken from the first fragment. `loop`/`max` may each be set by
// at most one fragment — ambiguous otherwise, since there'd be no principled
// way to pick a winner — and a step inside a `parallel` group can never be
// `entry`, regardless of how many fragments are visible here.
//
// Deliberately not checked here: the merged Body carrying exactly one
// `entry step`. Unlike the two rules above, "wrong entry count" is only a
// real problem once every fragment is actually visible together, and this
// function has no way to tell "a lone fragment, its siblings not pulled in
// by whatever's resolving imports right now" (zero entries, expected, not a
// bug) apart from "someone forgot `entry` entirely" (also zero entries) —
// see runtime.FindPipeline, which validates it at the one point that
// distinction doesn't matter: about to actually run/describe the thing.
func mergePartialGroup(name, kind string, frags []*ast.Pipeline) (*ast.Pipeline, error) {
	out := &ast.Pipeline{Pos: frags[0].Pos, Partial: true, Kind: kind, Name: name}

	loopSetBy, maxSetBy := "", ""
	for _, f := range frags {
		if f.Loop {
			if loopSetBy != "" {
				return nil, fmt.Errorf("partial %s %q: `loop` is declared on more than one fragment", kind, name)
			}
			loopSetBy = "x"
			out.Loop = true
		}
		if f.Max != "" {
			if maxSetBy != "" {
				return nil, fmt.Errorf("partial %s %q: `max` is declared on more than one fragment", kind, name)
			}
			maxSetBy = "x"
			out.Max = f.Max
		}
		if f.Param != nil || f.Returns != nil {
			if out.Param != nil || out.Returns != nil {
				return nil, fmt.Errorf("partial %s %q: a typed signature is declared on more than one fragment", kind, name)
			}
			out.Param, out.Returns = f.Param, f.Returns
		}
		out.Body = append(out.Body, f.Body...)
		for _, m := range f.Body {
			if m.Parallel != nil {
				for _, s := range m.Parallel.Steps {
					if s.Entry {
						return nil, fmt.Errorf("partial %s %q: step %q can't be `entry` inside a `parallel` group", kind, name, s.Name)
					}
				}
			}
		}
	}
	return out, nil
}
