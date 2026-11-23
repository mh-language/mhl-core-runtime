package lint

import (
	"github.com/mh-language/mhl-core-runtime/internal/features/prompt"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// checkPromptVariants checks every arm of a prompt that declares `for`
// variants (docs/MHL-PROMPT-VARIANTS.md): its body must be text, and every
// ${placeholder} in it must name one of the prompt's parameters — which all
// arms share. Call sites only ever render the default statically
// (renderPromptCall), and an agent picks an arm at run time, so without this
// a typo in a variant would first surface on the one harness that selects it.
//
// It runs on the merged program, after imports loaded the arms' files; a
// prompt without variants is left to the call-site checks exactly as before.
func checkPromptVariants(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		p := decl.Prompt
		if p == nil || len(p.Variants) == 0 {
			continue
		}
		if p.Body != nil {
			if err := prompt.CheckBody(p, nil); err != nil {
				findings = append(findings, Finding{File: fileAt(file, p.Pos), Line: p.Pos.Line, Column: p.Pos.Column, Message: err.Error()})
			}
		}
		for _, v := range p.Variants {
			if v.Body == nil {
				continue // a `from` file that failed to load is already reported
			}
			if err := prompt.CheckBody(p, v); err != nil {
				findings = append(findings, Finding{File: fileAt(file, v.Pos), Line: v.Pos.Line, Column: v.Pos.Column, Message: err.Error()})
			}
		}
	}
	return findings
}
