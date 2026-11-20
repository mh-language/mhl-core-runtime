package lint

import (
	"fmt"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// checkGates checks every typed pause — a gate, `pause(reason, decision: T)`
// — in a workflow's steps (interpreter/gate.go):
//
//   - pause takes no named argument but `decision:`;
//   - `decision:` names a type: a declared `type` or `enum`, or
//     string/number/bool — a resume's decision is checked against it;
//   - a gate is not in a `loop` workflow, where a resume can't answer it yet.
func checkGates(file string, prog *ast.Program) []Finding {
	var findings []Finding
	aliases, _ := types.Aliases(prog)
	for _, decl := range prog.Decls {
		p := decl.Pipeline
		if p == nil {
			continue
		}
		for _, m := range p.Body {
			for _, st := range pipelineMemberSteps(m) {
				walkPostfixes(st.Body, st.Pos, func(pf *ast.Postfix, pos lexer.Position) {
					if pf.Primary.Ident != "pause" || len(pf.Ops) == 0 || pf.Ops[0].Call == nil {
						return
					}
					at := func(msg string) {
						findings = append(findings, Finding{File: fileAt(file, pos), Line: pos.Line, Column: pos.Column, Message: msg})
					}
					for _, a := range pf.Ops[0].Call.Args {
						if a.Name == "" {
							continue
						}
						if a.Name != "decision" {
							at(fmt.Sprintf("pause: unknown argument %q — pause takes a reason and an optional `decision: Type`", a.Name))
							continue
						}
						name, ok := ast.IdentValue(a.Value)
						if !ok {
							at("pause: `decision:` expects a type name — a declared `type` or `enum`, or string/number/bool")
							continue
						}
						if _, known := aliases[name]; !known {
							if _, builtin := types.Parse(name); !builtin {
								at(fmt.Sprintf("pause: `decision: %s` — %q is not a declared type or enum", name, name))
								continue
							}
						}
						if p.Loop {
							at(fmt.Sprintf("pause(..., decision: %s) in loop %s %q: a resume can't answer a gate in a loop yet — use a plain pause(reason) with inputs, or move the gate out of the loop", name, p.Kind, p.Name))
						}
					}
				})
			}
		}
	}
	return findings
}
