package external

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// docBlockPattern recovers the inner text of a leading `/** ... **/` block —
// the kind-level documentation. parser.Parse already strips this same shape
// out of the source before lexing (see its stripLeadingDocBlock); this
// package only needs the text back, read straight off the raw source
// independently of parsing.
var docBlockPattern = regexp.MustCompile(`(?s)\A\s*/\*\*(.*?)\*\*/`)

// docLinePrefix opens a trailing `/// ...` doc-comment on one source line —
// the inline documentation form an Extensible property or method signature
// carries. It is deliberately not a grammar token: `///` already lexes as
// an ordinary (elided) mhl line comment (the third `/` is just more comment
// text to the lexer), so the parser only ever sees a bare `name: Type` /
// `name(...) -> Type`; this package recovers the doc text by re-reading the
// original source line by number and taking everything after the prefix,
// trimmed — no quoting or escaping, since a line comment already runs to
// end of line.
const docLinePrefix = "///"

// loadExtensibleManifest reads a single-file `extensible <kind> { manifest:
// {...} properties: {...} <methods> }` declaration — the mhl-native
// alternative to an extension.json plus a declarations sidecar — and
// projects it onto the same Manifest ordinary extension.json parses into,
// so every other part of the host (discovery, install, LoadManifest's
// caller, Manifest's methods) treats the two forms identically.
func loadExtensibleManifest(path string) (*Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	source := string(raw)
	kindDoc := docBlockText(source)

	prog, err := parser.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("extensible file %s: %w", path, err)
	}
	decl, err := soleExtensible(prog)
	if err != nil {
		return nil, fmt.Errorf("extensible file %s: %w", path, err)
	}

	m := &Manifest{dir: filepath.Dir(path), manifestPath: path}
	spec := extension.DeclarationSpec{Kind: decl.Kind, Documentation: kindDoc}
	lines := strings.Split(source, "\n")
	docAt := func(line int) string { return docCommentOnLine(lines, line) }

	manifestSeen := false
	for _, item := range decl.Items {
		switch {
		case item.Manifest != nil:
			if manifestSeen {
				return nil, fmt.Errorf("extensible file %s: \"manifest\" declared twice", path)
			}
			manifestSeen = true
			if err := applyManifestLiteral(m, item.Manifest); err != nil {
				return nil, fmt.Errorf("extensible file %s: %w", path, err)
			}
		case item.Properties != nil:
			for _, p := range item.Properties {
				spec.Properties = append(spec.Properties, extension.PropertySpec{
					Name:          p.Name,
					Type:          p.Type.String(),
					Documentation: docAt(p.Pos.Line),
				})
			}
		case item.Method != nil:
			spec.Methods = append(spec.Methods, methodSpecFromAST(item.Method, docAt(item.Method.Pos.Line)))
		}
	}
	if !manifestSeen {
		return nil, fmt.Errorf("extensible file %s: missing a \"manifest: { ... }\" block", path)
	}

	m.Declares = []extension.DeclarationSpec{spec}
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("extensible file %s: %w", path, err)
	}
	return m, nil
}

// soleExtensible returns prog's one top-level `extensible` declaration —
// an error naming the file as having none or more than one.
func soleExtensible(prog *ast.Program) (*ast.Extensible, error) {
	var found *ast.Extensible
	for _, decl := range prog.Decls {
		if decl.Extensible == nil {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("more than one \"extensible\" declaration")
		}
		found = decl.Extensible
	}
	if found == nil {
		return nil, fmt.Errorf(`no "extensible <kind> { ... }" declaration found`)
	}
	return found, nil
}

// methodSpecFromAST builds an extension.MethodSpec from a parsed
// ExtensibleMethod, rendering Signature the same "name(params) -> returns"
// shape a hand-written JSON/mhl declarations sidecar uses.
func methodSpecFromAST(meth *ast.ExtensibleMethod, doc string) extension.MethodSpec {
	returns := meth.Returns.String()
	ms := extension.MethodSpec{Name: meth.Name, Returns: returns, Documentation: doc}
	paramTexts := make([]string, 0, len(meth.Params))
	for _, p := range meth.Params {
		ps := extension.ParamSpec{Name: p.Name, Optional: p.Default != nil}
		text := p.Name
		if p.Type != nil {
			ps.Type = p.Type.String()
			text += ": " + ps.Type
		}
		ms.Params = append(ms.Params, ps)
		paramTexts = append(paramTexts, text)
	}
	ms.Signature = fmt.Sprintf("%s(%s) -> %s", meth.Name, strings.Join(paramTexts, ", "), returns)
	return ms
}

// applyManifestLiteral reads expr (the object literal after `manifest:`) as
// plain data via ast.LiteralValue, then round-trips it through encoding/json
// into Manifest's own fields — the same decoding an extension.json's top
// level already gets, so a hand-written manifest object and a JSON manifest
// are validated identically.
func applyManifestLiteral(m *Manifest, expr *ast.Expr) error {
	value, ok := ast.LiteralValue(expr)
	if !ok {
		return fmt.Errorf("\"manifest\" must be a literal object, e.g. { id: \"...\", ... }")
	}
	if _, isObj := value.(map[string]any); !isObj {
		return fmt.Errorf("\"manifest\" must be a literal object, e.g. { id: \"...\", ... }")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var fields struct {
		ID         string      `json:"id"`
		Version    string      `json:"version"`
		APIVersion string      `json:"api_version"`
		Executable string      `json:"executable"`
		Args       []string    `json:"args"`
		Env        []string    `json:"env"`
		Perms      Permissions `json:"permissions"`
	}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return err
	}
	m.ID = fields.ID
	m.Version = fields.Version
	m.APIVersion = fields.APIVersion
	m.Executable = fields.Executable
	m.Args = fields.Args
	m.Env = fields.Env
	m.Perms = fields.Perms
	return nil
}

// docBlockText returns the reflowed text of source's leading `/** ... **/`
// block, or "" when it has none. "Reflowed" turns the block's inner text —
// typically written one sentence-fragment per line for readability — into
// the single flowing prose string extension.DeclarationSpec.Documentation
// expects.
func docBlockText(source string) string {
	match := docBlockPattern.FindStringSubmatch(source)
	if match == nil {
		return ""
	}
	var parts []string
	for _, line := range strings.Split(match[1], "\n") {
		if line = strings.TrimSpace(line); line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, " ")
}

// docCommentOnLine reads the 1-based line'th entry of lines and, if its
// comment (if any) opens with docLinePrefix, returns the trimmed text after
// it. "" when the line is out of range, carries no comment at all, or
// carries an ordinary `//` comment that isn't the `///` doc form — found by
// locating the line's first "//" (where any comment starts) and checking
// that specific occurrence, so a plain comment that happens to contain
// "///" later in its text is correctly left uncaptured.
func docCommentOnLine(lines []string, line int) string {
	if line < 1 || line > len(lines) {
		return ""
	}
	text := lines[line-1]
	i := strings.Index(text, "//")
	if i < 0 || !strings.HasPrefix(text[i:], docLinePrefix) {
		return ""
	}
	return strings.TrimSpace(text[i+len(docLinePrefix):])
}
