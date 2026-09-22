package lsp

import (
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// toolMethodSig resolves a symTool method's real signature — Params (with
// their declared types and which are defaulted/optional) and Returns —
// from its actual declaration, instead of the "nothing static is known"
// signatureForMethod always returned for symTool before this (every other
// symbol kind reads its signature from a hand-written or adapter-derived
// table; a user's own `tool` never had one at all).
//
// It isolates just toolName's own body text (locateDeclaration finds the
// declaration by a regex scan, not a full parse, so this works even while
// the call the cursor is inside — the very thing signature help is being
// asked about — has its own "(" not yet closed anywhere else in the
// buffer) and reparses that one snippet standalone via parser.Parse. A
// user type alias referenced by a param's annotation doesn't need
// resolving here: the label only needs the annotation's own surface syntax
// (ast.TypeExpr.String()), not its underlying shape.
func toolMethodSig(path, text, toolName, method string) (sig, bool) {
	_, src, nameOff, kind, ok := locateDeclaration(path, text, toolName, nil)
	if !ok || kind != symTool {
		return sig{}, false
	}
	bodyStart, bodyEnd, ok := blockBounds(src, nameOff)
	if !ok {
		return sig{}, false
	}
	prog, err := parser.Parse("tool T {" + src[bodyStart:bodyEnd] + "}")
	if err != nil || len(prog.Decls) == 0 || prog.Decls[0].Tool == nil {
		return sig{}, false
	}
	for _, m := range prog.Decls[0].Tool.Methods {
		if m.Name == method {
			return sigFromParams(method, m.Params, m.Returns), true
		}
	}
	return sig{}, false
}

// promptCallSig resolves a declared `prompt Name(...)`'s own call signature
// the same way toolMethodSig does for a tool method: isolate just the
// parameter list's source span (Params sit before the body, whether that
// body is `{ ... }` or `from "..."` — neither needs parsing here) and
// reparse that snippet standalone.
func promptCallSig(path, text, promptName string) (sig, bool) {
	_, src, nameOff, kind, ok := locateDeclaration(path, text, promptName, nil)
	if !ok || kind != symPrompt {
		return sig{}, false
	}
	openRel := strings.IndexByte(src[nameOff:], '(')
	if openRel < 0 {
		return sig{}, false
	}
	open := nameOff + openRel
	closeParen, ok := matchParen(src, open)
	if !ok {
		return sig{}, false
	}
	prog, err := parser.Parse("prompt P" + src[open:closeParen+1] + ` { "" }`)
	if err != nil || len(prog.Decls) == 0 || prog.Decls[0].Prompt == nil {
		return sig{}, false
	}
	return sigFromParams(promptName, prog.Decls[0].Prompt.Params, nil), true
}

// memorySig resolves a symMemory method's doc to the specific text for
// memName's own declared backend (memoryMethodSigsByType), falling back to
// memoryMethodSigs' generic, all-backends paragraph when the backend can't
// be determined (an unresolvable declaration — e.g. the ephemeral `mem`
// keyword, which isn't a `memory X { ... }` declaration at all) or the
// method has no backend-specific entry. memoryMethodSigs' own Label/Params
// stay the base case throughout; only Doc is ever replaced, since the
// generic Label (covering kv/json's two overloads and append_log/jsonl's
// two payload shapes at once) is still accurate for any one backend, just
// more general than that backend alone needs.
func memorySig(path, text, memName, method string) (sig, bool) {
	base, ok := memoryMethodSigs[method]
	if !ok {
		return sig{}, false
	}
	if byType, ok := memoryMethodSigsByType[memoryDeclaredType(path, text, memName)]; ok {
		if doc, ok := byType[method]; ok {
			base.Doc = doc
		}
	}
	return base, true
}

// memoryDeclaredType isolates memName's own declaration body — the same
// locateDeclaration + blockBounds + standalone reparse toolMethodSig uses —
// and reads its `type:` property. "" for kv (the type-less kv default,
// see ast/language-design.md) is intentionally the same zero value as "not
// found", since memoryMethodSigsByType has no "" entry either — kv's Doc
// just stays memoryMethodSigs' generic base text; the difference isn't
// worth a table entry to express.
func memoryDeclaredType(path, text, memName string) string {
	_, src, nameOff, kind, ok := locateDeclaration(path, text, memName, nil)
	if !ok || kind != symMemory {
		return ""
	}
	bodyStart, bodyEnd, ok := blockBounds(src, nameOff)
	if !ok {
		return ""
	}
	prog, err := parser.Parse("memory M {" + src[bodyStart:bodyEnd] + "}")
	if err != nil || len(prog.Decls) == 0 || prog.Decls[0].Memory == nil {
		return ""
	}
	for _, p := range prog.Decls[0].Memory.Props {
		if p.Name == "type" {
			t, _ := ast.StringValue(p.Value)
			return t
		}
	}
	return ""
}

// memoryMethodSigsByType supplies memorySig's backend-specific Doc text —
// memoryMethodSigs' own entries describe every backend's get/set/append at
// once (necessarily denser, since a reader landing there has no declared
// memory to narrow it down, e.g. hovering the bare `memory`/`mem` grammar
// keyword itself has no receiver to resolve a type from); this is the
// focused version shown once memorySig knows which one backend a given
// `Session.get(...)` call is actually talking to.
var memoryMethodSigsByType = map[string]map[string]string{
	"kv": {
		"get": "Reads the value stored at `key`, or `default` (`null` when omitted) if it isn't set. Never raises on a missing key.",
		"set": "Writes `key` to `value` and returns it.",
	},
	"json": {
		"get": "Reads the value at `key` — navigate nested objects/arrays with `::` segments, e.g. `Session.get(\"config::retries\")`. Returns `default` (`null` when omitted) if the path doesn't resolve.",
		"set": "Writes `key` to `value` (same `::` nested-path navigation as `get`) and returns it. Given a single object argument instead of key/value, bulk-replaces the whole store with it and returns that object.",
	},
	"append_log": {
		"append": "Appends `value` (a string) as one new text line. Returns the value that was written.",
	},
	"jsonl": {
		"append": "Appends `value` (any JSON-encodable value) as one new JSON line. Returns the value that was written.",
	},
}

// sigFromParams renders name/params/returns into a sig the same style every
// hand-written entry in signatures.go already uses — "name?: Type" for a
// defaulted (so caller-optional) parameter, "name: Type" for a required
// one, an untyped parameter's name bare. The default's own value isn't
// printed (nothing in this package prints an *ast.Expr back to source
// today), only whether one exists.
func sigFromParams(name string, params []*ast.Param, returns *ast.TypeExpr) sig {
	parts := make([]string, len(params))
	names := make([]string, len(params))
	for i, p := range params {
		part := p.Name
		if p.Default != nil {
			part += "?"
		}
		if p.Type != nil {
			part += ": " + p.Type.String()
		}
		parts[i] = part
		names[i] = p.Name
	}
	label := name + "(" + strings.Join(parts, ", ") + ")"
	if returns != nil {
		label += ": " + returns.String()
	}
	return sig{Label: label, Params: names}
}

// matchParen returns the index of the ")" matching the "(" at src[open],
// depth- and string-literal-aware (a parameter default's own string value
// may itself contain unbalanced parens, e.g. `mode: string = "(default)"`).
func matchParen(src string, open int) (close int, ok bool) {
	depth := 0
	inString := false
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '"':
			if i == 0 || src[i-1] != '\\' {
				inString = !inString
			}
		case '(':
			if !inString {
				depth++
			}
		case ')':
			if !inString {
				depth--
				if depth == 0 {
					return i, true
				}
			}
		}
	}
	return 0, false
}
