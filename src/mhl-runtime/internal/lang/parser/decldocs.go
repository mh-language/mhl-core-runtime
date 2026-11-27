package parser

import (
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// attachDeclDocs gives every pipeline/workflow and workflow alias its
// documentation — the contiguous `///` lines directly above the declaration
// (above `export`/`internal`/`partial`/`loop` when it has them):
//
//	/// Reviews a diff and reports whether it can merge.
//	///
//	/// Comments are posted on the PR; nothing is merged.
//	workflow Review { ... }
//
// Lines of one paragraph are joined with a space; an empty `///` line
// separates paragraphs ("\n\n"), so a multi-paragraph description reaches an
// MCP client with its breaks. Unlike an input's doc, no leading "Name:" is
// dropped: a declaration's doc is published as written.
func attachDeclDocs(prog *ast.Program, source string) {
	var lines []string
	for _, d := range prog.Decls {
		if (d.Pipeline == nil && d.Alias == nil) || d.Pos.Line <= 0 {
			continue
		}
		if lines == nil {
			lines = strings.Split(source, "\n")
		}
		doc := paragraphsAbove(lines, d.Pos.Line)
		if d.Pipeline != nil {
			d.Pipeline.Doc = doc
		} else {
			d.Alias.Doc = doc
		}
	}
}

// paragraphsAbove is the `///` block right above 1-based line, in reading
// order, as paragraphs: a `////` line, a plain `//` line, a blank line or
// code ends the block, as for docAbove.
func paragraphsAbove(lines []string, line int) string {
	var block []string
	for i := line - 2; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(t, docPrefix) || strings.HasPrefix(t, docPrefix+"/") {
			break
		}
		block = append(block, strings.TrimSpace(strings.TrimPrefix(t, docPrefix)))
	}
	var paras []string
	var cur []string
	for i := len(block) - 1; i >= 0; i-- {
		if block[i] == "" {
			if len(cur) > 0 {
				paras = append(paras, strings.Join(cur, " "))
				cur = nil
			}
			continue
		}
		cur = append(cur, block[i])
	}
	if len(cur) > 0 {
		paras = append(paras, strings.Join(cur, " "))
	}
	return strings.Join(paras, "\n\n")
}
