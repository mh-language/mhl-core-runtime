package lsp

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/mhfile"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// inputHover describes the workflow input named word — where it is declared
// and when the run asks for it — with its `///` documentation
// (ast.PipelineInput.Doc). The input is looked up in this file's pipelines,
// then in the other fragments of a partial one: the .mh files of this folder
// and of its parent (an entry fragment importing `story/x.mh` lives one up).
// nil when no input of that name is found.
func inputHover(path, text, word string) *hover {
	prog, err := parser.Parse(text)
	if err != nil {
		return nil
	}
	names := map[string]bool{}
	for _, d := range prog.Decls {
		if p := d.Pipeline; p != nil {
			if in := findInput(p, word); in != nil {
				return inputHoverOf(p, in)
			}
			if p.Partial {
				names[p.Name] = true
			}
		}
	}
	if len(names) == 0 {
		return nil
	}
	dir := filepath.Dir(path)
	for _, d := range []string{dir, filepath.Dir(dir)} {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			full := filepath.Join(d, e.Name())
			if e.IsDir() || !mhfile.Is(e.Name()) || full == path {
				continue
			}
			src, err := os.ReadFile(full)
			if err != nil {
				continue
			}
			sp, err := parser.Parse(string(src))
			if err != nil {
				continue
			}
			for _, decl := range sp.Decls {
				p := decl.Pipeline
				if p == nil || !p.Partial || !names[p.Name] {
					continue
				}
				if in := findInput(p, word); in != nil {
					return inputHoverOf(p, in)
				}
			}
		}
	}
	return nil
}

func findInput(p *ast.Pipeline, name string) *ast.PipelineInput {
	for _, m := range p.Body {
		if m.Input != nil && m.Input.Name == name {
			return m.Input
		}
	}
	return nil
}

func inputHoverOf(p *ast.Pipeline, in *ast.PipelineInput) *hover {
	decl := "input " + in.Name + ": " + in.Type.String()
	parts := []string{"```mhl\n" + decl + "\n```"}
	if in.Doc != "" {
		parts = append(parts, in.Doc)
	}
	when := "Asked for when the run of `" + p.Name + "` starts."
	if in.Stage != "" {
		when = "Asked for on entering stage `" + in.Stage + "` of `" + p.Name + "`."
	}
	if in.Default != nil {
		when += " Has a default."
	}
	parts = append(parts, when)
	return &hover{Contents: markupContent{Kind: "markdown", Value: strings.Join(parts, "\n\n")}}
}
