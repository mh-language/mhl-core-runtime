package lsp

import (
	"fmt"
	"path/filepath"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// diagnosticsFor statically checks text as the .mh file at path (same
// engine `mhl lint` uses — see internal/lang/lint.Source) and converts
// every Finding into an LSP diagnostic. A finding in another file — an
// imported module, labelled with its own file by lint — has no position in
// this document: it is shown on the first line, its real file and line in
// the message, instead of at that module's line number here.
func diagnosticsFor(path, text string) []diagnostic {
	findings := lint.Source(path, text)
	diags := make([]diagnostic, 0, len(findings))
	for _, f := range findings {
		if f.File != "" && filepath.Clean(f.File) != filepath.Clean(path) {
			f.Message = fmt.Sprintf("%s:%d: %s", f.File, f.Line, f.Message)
			f.Line, f.Column = 1, 1
		}
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
