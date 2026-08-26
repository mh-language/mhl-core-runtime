package lsp

import "github.com/mh-language/mhl-core-runtime/internal/lang/lint"

// diagnosticsFor statically checks text as the .mh file at path (same
// engine `mhl lint` uses — see internal/lang/lint.Source) and converts
// every Finding into an LSP diagnostic.
func diagnosticsFor(path, text string) []diagnostic {
	findings := lint.Source(path, text)
	diags := make([]diagnostic, 0, len(findings))
	for _, f := range findings {
		line := f.Line - 1
		if line < 0 {
			line = 0
		}
		col := f.Column - 1
		if col < 0 {
			col = 0
		}
		diags = append(diags, diagnostic{
			Range: rangeT{
				Start: position{Line: line, Character: col},
				End:   position{Line: line, Character: col + 1},
			},
			Severity: 1,
			Source:   "mhl",
			Message:  f.Message,
		})
	}
	return diags
}
