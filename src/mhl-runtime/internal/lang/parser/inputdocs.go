package parser

import (
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// docPrefix opens a doc comment line. To the lexer `///` is an ordinary line
// comment (the third `/` is comment text), so the doc is recovered from the
// source text by position, after parsing — as the extensible manifest reads
// its trailing `///` docs.
const docPrefix = "///"

// attachInputDocs gives every `input` — a pipeline/workflow's and an
// agent's — its documentation: the contiguous `///` lines directly above it,
// each trimmed, joined with a space (PipelineInput.Doc). A plain `//` line,
// a blank line or code above stops the block. A leading `name:` that repeats
// the input's own name (`/// repo: the repository`) is dropped: the name is
// already shown beside the doc everywhere it appears.
func attachInputDocs(prog *ast.Program, source string) {
	lines := strings.Split(source, "\n")
	attach := func(in *ast.PipelineInput) {
		if in != nil && in.Pos.Line > 0 {
			in.Doc = docAbove(lines, in.Pos.Line, in.Name)
		}
	}
	for _, d := range prog.Decls {
		if d.Pipeline != nil {
			for _, m := range d.Pipeline.Body {
				attach(m.Input)
			}
		}
		if d.Agent != nil {
			for _, in := range d.Agent.Inputs {
				attach(in)
			}
		}
	}
}

// docAbove collects the `///` lines right above 1-based line, in reading
// order, without a leading "name:" equal to name.
func docAbove(lines []string, line int, name string) string {
	var block []string
	for i := line - 2; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(t, docPrefix) || strings.HasPrefix(t, docPrefix+"/") {
			break
		}
		block = append(block, strings.TrimSpace(strings.TrimPrefix(t, docPrefix)))
	}
	if len(block) == 0 {
		return ""
	}
	for l, r := 0, len(block)-1; l < r; l, r = l+1, r-1 {
		block[l], block[r] = block[r], block[l]
	}
	doc := strings.TrimSpace(strings.Join(block, " "))
	if rest, ok := strings.CutPrefix(doc, name+":"); ok {
		doc = strings.TrimSpace(rest)
	}
	return doc
}
