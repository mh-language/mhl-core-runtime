package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/alecthomas/participle/v2"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/flow"
	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// graphStdin is where `mhl graph --stdin` reads the source; a var so tests
// can substitute it.
var graphStdin io.Reader = os.Stdin

// runGraph implements:
//
//	mhl graph <file.mh> [--stdin] [--merged [--root <entry.mh>]]
//
// It prints the file's control-flow graph (internal/lang/flow) as one JSON
// object: every workflow's steps, parallel groups and transitions with the
// byte spans they came from, the declarations a workflow can call, and the
// parse/lint diagnostics. With --stdin the source is read from stdin and
// <file.mh> only names it (imports resolve relative to it) — how an editor
// or visual tool graphs an unsaved buffer. A source that does not parse is not
// a command failure: it reports "ok": false with the diagnostics, exit 0.
//
// --merged projects each `partial` workflow the file declares from all of
// its fragments (flow.BuildMerged): those in the file's import closure, or
// in <entry.mh>'s with --root — how an editor shows a fragment that some
// other file imports as part of the whole workflow. Members from another
// fragment carry that file's absolute path in "file".
func runGraph(args []string, out io.Writer) error {
	var path, root string
	fromStdin, merged := false, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--stdin":
			fromStdin = true
		case a == "--merged":
			merged = true
		case a == "--root":
			if i+1 >= len(args) {
				return fmt.Errorf("graph: --root needs a file")
			}
			i++
			root = args[i]
		case len(a) > 1 && a[0] == '-':
			return fmt.Errorf("graph: unknown flag %q", a)
		case path == "":
			path = a
		default:
			return fmt.Errorf("graph: unexpected argument %q", a)
		}
	}
	if path == "" {
		return fmt.Errorf("usage: mhl graph <file.mh> [--stdin] [--merged [--root <entry.mh>]]")
	}
	if root != "" && !merged {
		return fmt.Errorf("graph: --root only applies with --merged")
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
	if merged && g.OK {
		mergePartials(g, path, string(data), root)
	}
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
		d := flow.Diagnostic{Line: f.Line, Column: f.Column, Message: f.Message, Severity: string(f.Severity)}
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

// mergePartials replaces each `partial` workflow in g — the graph of src,
// the content of path — with its projection from every fragment in root's
// import closure (path's own when root is ""), path's content taken from
// src so an unsaved buffer merges as typed. A fragment that can't be read
// or parsed becomes a diagnostic and the workflow keeps its one-file form.
func mergePartials(g *flow.Graph, path, src, root string) {
	self, _ := filepath.Abs(path)
	if root == "" {
		root = path
	}
	type file struct {
		abs, src string
		prog     *ast.Program
	}
	var files []file
	seen := map[string]bool{}
	var visit func(p string) error
	visit = func(p string) error {
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		if seen[abs] {
			return nil
		}
		seen[abs] = true
		text := src
		if abs != self {
			data, err := os.ReadFile(abs)
			if err != nil {
				return err
			}
			text = string(data)
		}
		prog, err := parser.Parse(text)
		if err != nil {
			return fmt.Errorf("%s: %w", abs, err)
		}
		// A run appends each import's declarations after the importer's
		// own, depth-first: the fragments' order is the files' order.
		files = append(files, file{abs: abs, src: text, prog: prog})
		for _, d := range prog.Decls {
			if d.Import != nil {
				if err := visit(filepath.Join(filepath.Dir(abs), d.Import.Path)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(root); err != nil {
		g.Diagnostics = append(g.Diagnostics, flow.Diagnostic{Line: 1, Column: 1, Message: "merged graph: " + err.Error()})
		return
	}
	if !seen[self] {
		g.Diagnostics = append(g.Diagnostics, flow.Diagnostic{Line: 1, Column: 1, Message: fmt.Sprintf("merged graph: %s does not import %s", root, path)})
		return
	}
	for i, w := range g.Workflows {
		if !w.Partial {
			continue
		}
		var frags []flow.Fragment
		for _, f := range files {
			for _, d := range f.prog.Decls {
				if d.Pipeline == nil || !d.Pipeline.Partial || d.Pipeline.Name != w.Name {
					continue
				}
				name := f.abs
				if f.abs == self {
					name = ""
				}
				frags = append(frags, flow.Fragment{File: name, Src: f.src, Prog: f.prog, Decl: d})
			}
		}
		m, err := flow.BuildMerged(frags)
		if err != nil {
			g.Diagnostics = append(g.Diagnostics, flow.Diagnostic{Line: 1, Column: 1, Message: "merged graph: " + err.Error()})
			continue
		}
		g.Workflows[i] = m
	}
}
