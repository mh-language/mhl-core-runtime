package lint

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
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
