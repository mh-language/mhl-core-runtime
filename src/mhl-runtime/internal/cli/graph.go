package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/alecthomas/participle/v2"

	"github.com/mh-language/mhl-core-runtime/internal/lang/flow"
	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// graphStdin is where `mhl graph --stdin` reads the source; a var so tests
// can substitute it.
var graphStdin io.Reader = os.Stdin

// runGraph implements:
//
//	mhl graph <file.mh> [--stdin]
//
// It prints the file's control-flow graph (internal/lang/flow) as one JSON
// object: every workflow's steps, parallel groups and transitions with the
// byte spans they came from, the declarations a workflow can call, and the
// parse/lint diagnostics. With --stdin the source is read from stdin and
// <file.mh> only names it (imports resolve relative to it) — how an editor
// or visual tool graphs an unsaved buffer. A source that does not parse is not
// a command failure: it reports "ok": false with the diagnostics, exit 0.
func runGraph(args []string, out io.Writer) error {
	var path string
	fromStdin := false
	for _, a := range args {
		switch {
		case a == "--stdin":
			fromStdin = true
		case len(a) > 1 && a[0] == '-':
			return fmt.Errorf("graph: unknown flag %q", a)
		case path == "":
			path = a
		default:
			return fmt.Errorf("graph: unexpected argument %q", a)
		}
	}
	if path == "" {
		return fmt.Errorf("usage: mhl graph <file.mh> [--stdin]")
	}
	var data []byte
	var err error
	if fromStdin {
		data, err = io.ReadAll(graphStdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return fmt.Errorf("graph: %w", err)
	}
	g := graphOf(path, string(data))
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(g)
}

// graphOf builds the `mhl graph` document for src, the content of path.
func graphOf(path, src string) *flow.Graph {
	empty := &flow.Graph{
		Version: flow.Version, File: path, Diagnostics: []flow.Diagnostic{},
		Imports: []flow.Import{}, Declarations: []flow.Declaration{}, Workflows: []flow.Workflow{},
	}
	diags := []flow.Diagnostic{}
	for _, f := range lint.Source(path, src) {
		d := flow.Diagnostic{Line: f.Line, Column: f.Column, Message: f.Message}
		if f.File != "" && filepath.Clean(f.File) != filepath.Clean(path) {
			d.File = f.File
		}
		diags = append(diags, d)
	}
	prog, err := parser.Parse(src)
	if err != nil {
		empty.Diagnostics = diags
		if len(diags) == 0 {
			d := flow.Diagnostic{Line: 1, Column: 1, Message: err.Error()}
			if perr, ok := err.(participle.Error); ok {
				d.Line, d.Column, d.Message = perr.Position().Line, perr.Position().Column, perr.Message()
			}
			empty.Diagnostics = []flow.Diagnostic{d}
		}
		return empty
	}
	g, err := flow.Build(prog, src)
	if err != nil {
		empty.Diagnostics = append(diags, flow.Diagnostic{Line: 1, Column: 1, Message: err.Error()})
		return empty
	}
	g.OK = true
	g.File = path
	g.Diagnostics = diags
	return g
}
