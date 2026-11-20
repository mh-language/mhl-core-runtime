package parser

import "github.com/mh-language/mhl-core-runtime/internal/lang/ast"

// expandStages applies a fragment's header stage (`partial workflow X stage
// Name { ... }`) to its members: every step and parallel group of the body is
// tagged with the stage's name (Step.Stage, ParallelGroup.Stage), and the
// stage itself is recorded in Pipeline.Stages. Everything downstream — the
// runtime, lint, the interpreter, the digest — walks Body and sees plain
// members carrying a name, so a stage changes nothing about how they run.
func expandStages(prog *ast.Program) {
	for _, d := range prog.Decls {
		p := d.Pipeline
		if p == nil || p.Stage == "" {
			continue
		}
		st := &ast.Stage{Pos: p.Pos, EndPos: p.EndPos, Name: p.Stage}
		for _, m := range p.Body {
			switch {
			case m.Step != nil:
				m.Step.Stage = p.Stage
				st.Members = append(st.Members, m.Step.Name)
			case m.Parallel != nil:
				m.Parallel.Stage = p.Stage
				st.Members = append(st.Members, m.Parallel.Name)
			}
		}
		p.Stages = append(p.Stages, st)
	}
}
