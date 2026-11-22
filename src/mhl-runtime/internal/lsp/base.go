package lsp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/mhfile"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// baseFragment finds the base fragment of partial workflow name — the one
// declaring its `entry step`/`entry route`, what `base.x` names — in text
// (the buffer at path), else in the .mh files of path's folder and of its
// parent (an entry fragment importing `story/x.mh` sits one folder up).
// Returns the fragment, its file and that file's source; ok false when none
// is found.
func baseFragment(path, text, name string) (frag *ast.Pipeline, file, src string, ok bool) {
	if prog, err := parser.Parse(text); err == nil {
		if p := entryFragment(prog, name); p != nil {
			return p, path, text, true
		}
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
			raw, err := os.ReadFile(full)
			if err != nil {
				continue
			}
			prog, err := parser.Parse(string(raw))
			if err != nil {
				continue
			}
			if p := entryFragment(prog, name); p != nil {
				return p, full, string(raw), true
			}
		}
	}
	return nil, "", "", false
}

// entryFragment is prog's partial fragment of name holding the entry.
func entryFragment(prog *ast.Program, name string) *ast.Pipeline {
	for _, d := range prog.Decls {
		p := d.Pipeline
		if p == nil || !p.Partial || p.Name != name {
			continue
		}
		for _, m := range p.Body {
			if (m.Step != nil && m.Step.Entry) || (m.Route != nil && m.Route.Entry) {
				return p
			}
		}
	}
	return nil
}

// baseMember is one name a base fragment declares: its keyword, type text
// (inputs), doc, and the byte offset of the name in its file.
type baseMember struct {
	kind, typ, doc string
	off            int
}

func baseMembers(frag *ast.Pipeline, src string) map[string]baseMember {
	out := map[string]baseMember{}
	at := func(pos int, name string) int {
		if pos < 0 || pos > len(src) {
			return pos
		}
		if i := strings.Index(src[pos:], name); i >= 0 {
			return pos + i
		}
		return pos
	}
	for _, m := range frag.Body {
		switch {
		case m.Input != nil:
			out[m.Input.Name] = baseMember{kind: "input", typ: m.Input.Type.String(), doc: m.Input.Doc, off: at(m.Input.Pos.Offset, m.Input.Name)}
		case m.Var != nil:
			out[m.Var.Name] = baseMember{kind: "var", off: at(m.Pos.Offset, m.Var.Name)}
		case m.Mem != nil:
			out[m.Mem.Name] = baseMember{kind: "mem", off: at(m.Pos.Offset, m.Mem.Name)}
		case m.Const != nil:
			out[m.Const.Name] = baseMember{kind: "const", off: at(m.Pos.Offset, m.Const.Name)}
		}
	}
	return out
}

// baseCompletionAt lists, after `base.` inside a step of a partial
// workflow, the inputs/vars/mems/consts its base fragment declares.
func baseCompletionAt(path, text string, pos position) []completionItem {
	name, ok := enclosingPipelineName(text, pos)
	if !ok {
		return nil
	}
	frag, file, src, ok := baseFragment(path, repairForParse(text, pos), name)
	if !ok {
		return nil
	}
	var items []completionItem
	for n, m := range baseMembers(frag, src) {
		it := completionItem{Label: n, Kind: kindVariable, Detail: m.kind + " · " + filepath.Base(file)}
		if m.doc != "" {
			it.Documentation = &markupContent{Kind: "markdown", Value: m.doc}
		}
		items = append(items, it)
	}
	return items
}

// baseMemberAt resolves `base.member` at pos to its declaration in the base
// fragment.
func baseMemberAt(path, text string, pos position, member string) (m baseMember, file, src string, ok bool) {
	name, ok := enclosingPipelineName(text, pos)
	if !ok {
		return baseMember{}, "", "", false
	}
	frag, file, src, ok := baseFragment(path, text, name)
	if !ok {
		return baseMember{}, "", "", false
	}
	m, ok = baseMembers(frag, src)[member]
	return m, file, src, ok
}

// baseHover describes `base.member`: what it is, where the base declares
// it, and its `///` doc.
func baseHover(path, text string, pos position, member string) *hover {
	m, file, src, ok := baseMemberAt(path, text, pos, member)
	if !ok {
		return nil
	}
	decl := m.kind + " " + member
	if m.typ != "" {
		decl += ": " + m.typ
	}
	parts := []string{"```mhl\n" + decl + "\n```"}
	if m.doc != "" {
		parts = append(parts, m.doc)
	}
	parts = append(parts, fmt.Sprintf("Declared in the base fragment, `%s:%d`.", filepath.Base(file), offsetToPos(src, m.off).Line+1))
	return &hover{Contents: markupContent{Kind: "markdown", Value: strings.Join(parts, "\n\n")}}
}

// baseDefinition is where the base fragment declares `base.member`.
func baseDefinition(path, text string, pos position, member string) (location, bool) {
	m, file, src, ok := baseMemberAt(path, text, pos, member)
	if !ok {
		return location{}, false
	}
	return location{URI: pathToURI(file), Range: identRange(src, m.off, member)}, true
}
