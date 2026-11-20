package lint

import (
	"fmt"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/flow"
)

// memberStage is the stage of the fragment a pipeline member was declared in
// ("" for none) — parser.expandStages leaves the name on the step or group.
func memberStage(m *ast.PipelineMember) string {
	switch {
	case m.Step != nil:
		return m.Step.Stage
	case m.Parallel != nil:
		return m.Parallel.Stage
	}
	return ""
}

// checkStages checks a pipeline's stages — each the header stage of one of
// its `partial` fragments (`partial workflow X stage Name { ... }`) — over
// its body as merged from every fragment:
//
//   - a stage name is not also a step's, a parallel group's or a route's —
//     `goto Name` would be ambiguous;
//   - a stage is declared once: its members are consecutive, so two
//     fragments sharing a stage must be imported one right after the other;
//   - a header stage is on a `partial` declaration: it names the phase a
//     fragment holds.
func checkStages(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		p := decl.Pipeline
		if p == nil {
			continue
		}
		if p.Stage != "" && !p.Partial {
			findings = append(findings, Finding{File: fileAt(file, p.Pos), Line: p.Pos.Line, Column: p.Pos.Column,
				Message: fmt.Sprintf("`stage %s` on the header of %s %q puts the whole declaration in one stage — a stage names the phase a `partial` fragment holds: split the %s into one `partial` fragment per phase", p.Stage, p.Kind, p.Name, p.Kind)})
		}
		names := map[string]string{}
		for _, m := range p.Body {
			switch {
			case m.Route != nil:
				names[m.Route.Name] = "route"
			case m.Parallel != nil:
				names[m.Parallel.Name] = "parallel group"
				for _, s := range m.Parallel.Steps {
					names[s.Name] = "step"
				}
			case m.Step != nil:
				names[m.Step.Name] = "step"
			}
		}
		closed := map[string]bool{}
		reported := map[string]bool{}
		current := ""
		for _, m := range p.Body {
			if m.Step == nil && m.Parallel == nil {
				continue
			}
			st := memberStage(m)
			if st != current && current != "" {
				closed[current] = true
			}
			if st != "" && st != current {
				at := func(msg string) {
					findings = append(findings, Finding{File: fileAt(file, m.Pos), Line: m.Pos.Line, Column: m.Pos.Column, Message: msg})
				}
				if kind, clash := names[st]; clash && !reported[st] {
					reported[st] = true
					at(fmt.Sprintf("stage %q of %s %q has the name of a %s — `goto %s` would be ambiguous; rename one", st, p.Kind, p.Name, kind, st))
				}
				if closed[st] && !reported["dup:"+st] {
					reported["dup:"+st] = true
					at(fmt.Sprintf("%s %q declares stage %q in fragments that are not consecutive — a stage is one phase: declare it on one fragment, or import the fragments sharing it one right after the other", p.Kind, p.Name, st))
				}
			}
			current = st
		}
	}
	return findings
}

// checkStageEntries warns about a `goto` that enters another stage at a step
// other than its first: from outside a stage, the stage's name is the way
// in (`goto Specification`), so the stage keeps the say over where it starts
// and other fragments don't depend on its steps' names. A warning, not an
// error — entering mid-stage is valid (and a dispatching first step is the
// alternative when a caller must skip ahead). The `goto` inside the stage,
// and one to the stage's first step, are fine.
func checkStageEntries(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		p := decl.Pipeline
		if p == nil {
			continue
		}
		stageOf, first := map[string]string{}, map[string]string{}
		for _, m := range p.Body {
			st := memberStage(m)
			if st == "" {
				continue
			}
			name := ""
			switch {
			case m.Step != nil:
				name = m.Step.Name
			case m.Parallel != nil:
				name = m.Parallel.Name
			}
			stageOf[name] = st
			if _, ok := first[st]; !ok {
				first[st] = name
			}
		}
		if len(first) == 0 {
			continue
		}
		check := func(from, target string, pos lexer.Position) {
			to := stageOf[target]
			if to == "" || to == from || first[to] == target {
				return
			}
			where := "outside any stage"
			if from != "" {
				where = fmt.Sprintf("stage %q", from)
			}
			findings = append(findings, Finding{File: fileAt(file, pos), Line: pos.Line, Column: pos.Column, Severity: SeverityWarning,
				Message: fmt.Sprintf("`goto %s` from %s enters stage %q at %s, not at its start (%s) — prefer `goto %s`, and let the stage decide where it begins", target, where, to, target, first[to], to)})
		}
		routes := flow.Routes(p)
		if r := p.EntryRoute(); r != nil {
			for _, a := range r.Arms {
				check("", a.Target, a.Pos)
			}
		}
		for _, m := range p.Body {
			from := memberStage(m)
			for _, st := range pipelineMemberSteps(m) {
				flow.WalkGotos(st.Body, routes, func(g flow.GotoRef) {
					pos := g.Stmt.Pos
					if g.Arm != nil && g.Route == nil {
						pos = g.Arm.Pos
					}
					check(from, g.Target, pos)
				})
			}
		}
	}
	return findings
}
