package lint

import (
	"fmt"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// checkDescriptions checks how a pipeline/workflow or a workflow alias
// spells its description. The `///` lines above the declaration are the
// one way now; the `description: "..."` property still works with a
// deprecation warning that shows the doc comment to write instead, and
// declaring both is an error — `mhl serve` publishes one description.
func checkDescriptions(file string, prog *ast.Program) []Finding {
	var findings []Finding
	check := func(kind, name, doc string, props []*ast.Property, header string) {
		for _, p := range props {
			if p.Name != "description" {
				continue
			}
			label := fmt.Sprintf("%s %q", kind, name)
			if doc != "" {
				findings = append(findings, Finding{File: fileAt(file, p.Pos), Line: p.Pos.Line, Column: p.Pos.Column,
					Message: fmt.Sprintf("%s: the description is declared twice — by the `///` lines above the declaration and by `description:`; remove `description:`", label)})
				continue
			}
			findings = append(findings, Finding{File: fileAt(file, p.Pos), Line: p.Pos.Line, Column: p.Pos.Column, Severity: SeverityWarning,
				Message: fmt.Sprintf("%s: `description: \"...\"` is deprecated — write it as `///` lines right above the declaration: %s", label, docSuggestion(p.Value, header))})
		}
	}
	for _, d := range prog.Decls {
		switch {
		case d.Pipeline != nil:
			p := d.Pipeline
			var props []*ast.Property
			for _, m := range p.Body {
				if m.Prop != nil {
					props = append(props, m.Prop)
				}
			}
			check(pipelineKind(p), p.Name, p.Doc, props, fmt.Sprintf("%s %s", p.Kind, p.Name))
		case d.Alias != nil:
			a := d.Alias
			check(a.Kind, a.Name, a.Doc, a.Props, fmt.Sprintf("%s %s = %s", a.Kind, a.Name, a.Target))
		}
	}
	return findings
}

// docSuggestion is the doc comment a `description:` value becomes, shown
// above header (`/// Reviews a diff. ↵ workflow Review`); a non-literal value
// gets a placeholder.
func docSuggestion(v *ast.Expr, header string) string {
	text, ok := ast.StringValue(v)
	if !ok || strings.TrimSpace(text) == "" {
		text = "<summary>"
	}
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		b.WriteString("`/// " + strings.TrimSpace(line) + "` ")
	}
	return b.String() + "above `" + header + "`"
}
