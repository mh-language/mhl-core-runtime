package lint

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// mergePartials collapses every `partial pipeline`/`partial workflow`
// fragment sharing a Name in prog.Decls (already import-merged by
// mergeImports, so a whole-file `import "..."` has already pulled a
// sibling fragment's declaration in) into one ast.Pipeline with one
// concatenated Body, replacing the group's declarations with a single
// merged one at the position of the first fragment. It returns a new
// *ast.Program (prog itself is left untouched, matching mergeImports), plus
// a Finding per validation problem: two fragments disagreeing on `pipeline`
// vs `workflow`, `loop`/`max` declared on more than one fragment, an `entry
// step` (ast.Step.Entry) on a step inside a `parallel` group, or — only
// when this pass actually merged more than one fragment together — the
// merged set not carrying exactly one `entry step`. See mergePartialGroup's
// doc comment for why "found 0" is silent for a single, standalone
// fragment specifically (a real false-positive reported against production
// code once this shipped: every fragment file showed a permanent error the
// moment it was opened, since the LSP lints each open document on its own).
//
// Mirrored, not shared, by interpreter.resolvePartials — see mergeImports'
// doc comment for why lint keeps its own copy of this class of pass instead
// of calling into internal/engine (lint must not depend on engine; see
// src/mhl-runtime/README.md's dependency table).
func mergePartials(file string, prog *ast.Program) (*ast.Program, []Finding) {
	type group struct {
		firstIdx int
		kind     string
		frags    []*ast.Pipeline
	}
	groups := map[string]*group{}
	var order []string
	var findings []Finding

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
			findings = append(findings, Finding{File: file, Line: p.Pos.Line, Column: p.Pos.Column,
				Message: fmt.Sprintf("partial %s %q: one fragment declares it %q, another %q", g.kind, p.Name, g.kind, p.Kind)})
			continue
		}
		g.frags = append(g.frags, p)
	}

	merged := make(map[string]*ast.Pipeline, len(order))
	for _, name := range order {
		g := groups[name]
		m, mf := mergePartialGroup(file, name, g.kind, g.frags)
		findings = append(findings, mf...)
		merged[name] = m
	}

	out := &ast.Program{Decls: make([]*ast.Declaration, 0, len(prog.Decls))}
	for i, decl := range prog.Decls {
		if decl.Pipeline == nil || !decl.Pipeline.Partial {
			out.Decls = append(out.Decls, decl)
			continue
		}
		g := groups[decl.Pipeline.Name]
		if g == nil || i != g.firstIdx {
			continue // a later fragment of an already-emitted (or kind-mismatched) group
		}
		out.Decls = append(out.Decls, &ast.Declaration{Export: decl.Export, Pipeline: merged[decl.Pipeline.Name]})
	}
	return out, findings
}

// mergePartialGroup combines frags (every fragment of one partial
// pipeline/workflow, in declaration order) into a single ast.Pipeline,
// reporting every validation problem found rather than stopping at the
// first — consistent with the rest of this package's Finding-collecting
// style. The returned Pipeline is still usable even when findings is
// non-empty (best-effort, the same way a broken import doesn't stop
// mergeImports from producing a program the remaining checks can still run
// against).
//
// "More than one entry step" is always reported, however many fragments are
// visible — a fragment declaring two itself, or two different fragments
// each declaring one, is unambiguously wrong regardless of what else might
// exist elsewhere. "Zero entry steps" is only reported when len(frags) > 1:
// that's the one case where this pass has real evidence it's looking at the
// complete, assembled picture (something here actually pulled in a sibling
// via a whole-file `import`), so "nobody marked an entry anywhere" is a
// genuine bug worth flagging. A lone fragment (len(frags) == 1, the file
// linted on its own, its siblings never pulled in from here) legitimately
// has zero and that alone is not a defect in this file — silently allowing
// it here trades away lint catching a *truly* forgotten `entry` from a
// single-fragment file one keystroke earlier; runtime.FindPipeline still
// catches that loudly the moment anything tries to actually run it (a real
// `mhl run`, or a test's `.run()`), so nothing is silently broken — it's
// just not flagged one step earlier for this one specific, inherently
// ambiguous shape.
func mergePartialGroup(file, name, kind string, frags []*ast.Pipeline) (*ast.Pipeline, []Finding) {
	out := &ast.Pipeline{Pos: frags[0].Pos, Partial: true, Kind: kind, Name: name}
	var findings []Finding

	loopSet, maxSet := false, false
	entries := 0
	for _, f := range frags {
		if f.Loop {
			if loopSet {
				findings = append(findings, Finding{File: file, Line: f.Pos.Line, Column: f.Pos.Column,
					Message: fmt.Sprintf("partial %s %q: `loop` is declared on more than one fragment", kind, name)})
			}
			loopSet = true
			out.Loop = true
		}
		if f.Max != "" {
			if maxSet {
				findings = append(findings, Finding{File: file, Line: f.Pos.Line, Column: f.Pos.Column,
					Message: fmt.Sprintf("partial %s %q: `max` is declared on more than one fragment", kind, name)})
			}
			maxSet = true
			out.Max = f.Max
		}
		if f.Param != nil || f.Returns != nil {
			if out.Param != nil || out.Returns != nil {
				findings = append(findings, Finding{File: file, Line: f.Pos.Line, Column: f.Pos.Column,
					Message: fmt.Sprintf("partial %s %q: a typed signature is declared on more than one fragment", kind, name)})
			} else {
				out.Param, out.Returns = f.Param, f.Returns
			}
		}
		out.Body = append(out.Body, f.Body...)
		for _, m := range f.Body {
			if m.Step != nil && m.Step.Entry {
				entries++
			}
			if m.Parallel != nil {
				for _, s := range m.Parallel.Steps {
					if s.Entry {
						findings = append(findings, Finding{File: file, Line: s.Pos.Line, Column: s.Pos.Column,
							Message: fmt.Sprintf("partial %s %q: step %q can't be `entry` inside a `parallel` group", kind, name, s.Name)})
					}
				}
			}
		}
	}
	if entries > 1 || (entries == 0 && len(frags) > 1) {
		findings = append(findings, Finding{File: file, Line: out.Pos.Line, Column: out.Pos.Column,
			Message: fmt.Sprintf("partial %s %q: expected exactly one `entry step` across its fragments, found %d", kind, name, entries)})
	}
	// Fallthrough across a fragment *boundary* — a step with no unconditional
	// `goto`/`fail(...)`/`pause(...)`/`break` at the end of its body, sitting
	// last in its own fragment while another fragment still follows — is the
	// one shape of "silently depends on physical order" this package flags.
	// Reordering steps *within* one fragment file is exactly as visible as
	// reordering them in an ordinary single-file workflow (see
	// alwaysDiverts' doc comment for why that case is never flagged: mhl's
	// execution model defaults to sequential fallthrough everywhere, `goto`
	// only adds an option), but reordering *fragments* relative to each
	// other doesn't touch either file's own text — this is precisely the
	// gap a real production incident fell into: a convergence step named
	// Done, with an empty body, sat last in one fragment while a sibling
	// fragment was still imported after it, so the run silently continued
	// into that sibling's first step once Done "finished" — firing an
	// unwanted LLM call and corrupting pipeline-scoped state, only failing
	// later for an unrelated-looking reason. Only for `workflow` — a
	// `partial pipeline` has no `goto` at all (lint error), so every
	// fragment boundary already *must* fall through by design, same as
	// alwaysDiverts' `workflow`-only scope.
	if kind == "workflow" {
		for j := 0; j < len(frags)-1; j++ {
			m := lastStepLikeMember(frags[j].Body)
			if m == nil || m.Step == nil || alwaysDiverts(m.Step.Body) {
				continue // no step to check, a `parallel` group (never flagged), or already safe
			}
			next := firstStepLikeName(frags[j+1].Body)
			if next == "" {
				continue // the next fragment declares no step either; nothing to name
			}
			findings = append(findings, Finding{File: file, Line: stepLine(m.Step), Column: 1,
				Message: fmt.Sprintf("step %q has no `goto`/`fail(...)`/`pause(...)`/`break` guaranteed at the end of its body and is the last step of its fragment, with another fragment still merged after it — it will silently fall through into %q when it finishes",
					m.Step.Name, next)})
		}
	}
	return out, findings
}

// checkPipelineEntry flags `entry` used on a step inside a pipeline/workflow
// that isn't `partial` — first-declared-wins is unambiguous there already
// (see ast.Step.Entry's doc comment), so a second way to say the same thing
// is rejected rather than silently accepted.
func checkPipelineEntry(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		if decl.Pipeline == nil || decl.Pipeline.Partial {
			continue
		}
		p := decl.Pipeline
		for _, m := range p.Body {
			for _, s := range pipelineMemberSteps(m) {
				if s.Entry {
					findings = append(findings, Finding{File: file, Line: s.Pos.Line, Column: s.Pos.Column,
						Message: fmt.Sprintf("step %q can't be `entry`: %s %q isn't `partial`", s.Name, p.Kind, p.Name)})
				}
			}
		}
	}
	return findings
}
