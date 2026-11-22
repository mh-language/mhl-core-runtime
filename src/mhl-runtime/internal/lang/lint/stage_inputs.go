package lint

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// checkStageInputs checks a pipeline's inputs over its body as merged from
// every fragment:
//
//   - an input is declared once — two fragments (or one twice) declaring
//     the same name would bind it twice, with possibly different types;
//   - a stage input (one declared in a `partial ... stage S` fragment) is
//     not in a `loop` workflow, where a run cannot pause to ask for it yet;
//   - its stage has a step or parallel group: the run asks for a stage's
//     inputs on entering it, so a stage with nothing to run never would.
func checkStageInputs(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		p := decl.Pipeline
		if p == nil {
			continue
		}
		runs := map[string]bool{}
		for _, m := range p.Body {
			if st := memberStage(m); st != "" {
				runs[st] = true
			}
		}
		seen := map[string]bool{}
		for _, m := range p.Body {
			in := m.Input
			if in == nil {
				continue
			}
			at := func(msg string) {
				findings = append(findings, Finding{File: fileAt(file, in.Pos), Line: in.Pos.Line, Column: in.Pos.Column, Message: msg})
			}
			if seen[in.Name] {
				at(fmt.Sprintf("%s %q declares input %q more than once — declare it in one fragment", p.Kind, p.Name, in.Name))
			}
			seen[in.Name] = true
			if in.Stage == "" {
				continue
			}
			if p.Loop {
				at(fmt.Sprintf("input %q of stage %q in loop %s %q: a loop run cannot pause to ask for a stage's inputs yet — declare it in the entry fragment, outside every stage", in.Name, in.Stage, p.Kind, p.Name))
			}
			if !runs[in.Stage] {
				at(fmt.Sprintf("input %q of stage %q: the stage has no step or parallel group, so the run would never enter it to ask for the input — move it to a fragment of the stage that runs, or outside every stage", in.Name, in.Stage))
			}
		}
	}
	return findings
}
