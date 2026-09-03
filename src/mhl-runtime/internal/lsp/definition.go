package lsp

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// definitionAt answers textDocument/definition: given the cursor at pos in
// path/text it resolves the token under the cursor to the location of its
// declaration. Like the rest of this package it is a best-effort text scan,
// not a real parse — it recognizes three cases:
//
//   - the cursor sits inside the path string of an `import { ... } from
//     "..."` (or `prompt X(...) from "..."`) clause → the referenced file
//     itself, at position 0:0;
//   - the cursor is on the member of a `Receiver.member` access → the `tool`
//     method (or `enum` variant) named `member` inside `Receiver`'s
//     declaration body; for a member with no source (an agent's `.run`, a
//     memory's `.get`) it falls back to `Receiver`'s own declaration;
//   - the cursor is on any other identifier → the top-level declaration
//     (agent / memory / tool / prompt / pipeline / workflow / extension /
//     type / enum) of that name.
//
// A declaration is looked for in the current buffer first, then the file an
// `import { name } from "..."` in that buffer points at (following `as`
// aliases and re-exports), then — as a loose fallback — every other .mh file
// one directory level deep. It returns nil when nothing resolves.
func definitionAt(path, text string, pos position) []location {
	line := lineAt(text, pos.Line)

	if target, ok := importPathAt(line, pos.Character); ok {
		if abs := resolveRelativePath(path, target); abs != "" {
			return []location{{URI: pathToURI(abs)}}
		}
		return nil
	}

	word, start, ok := identAt(line, pos.Character)
	if !ok {
		return nil
	}

	// "Receiver.word" with the cursor on word: resolve the member.
	if start >= 1 && line[start-1] == '.' {
		if receiver, ok := identEndingAt(line, start-1); ok {
			if loc, found := findMember(path, text, receiver, word); found {
				return []location{loc}
			}
		}
	}

	if loc, found := findDeclaration(path, text, word); found {
		return []location{loc}
	}
	return nil
}

// declLocRe matches a top-level `<keyword> <Name>` declaration line,
// capturing the keyword (group 1) and the declared name (group 2). It mirrors
// symbols.go's declRe but is anchored for FindAllStringSubmatchIndex so the
// name's byte offset is recoverable. extDeclLocRe is its `extension <kind>
// <Name>` counterpart (two identifiers), capturing the name in group 1.
var (
	declLocRe    = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?(?:loop[ \t]+)?(agent|memory|tool|prompt|pipeline|workflow|type|enum)[ \t]+([A-Za-z_][A-Za-z0-9_]*)`)
	extDeclLocRe = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?extension[ \t]+[A-Za-z_][A-Za-z0-9_]*[ \t]+([A-Za-z_][A-Za-z0-9_]*)`)
)

// findDeclaration resolves name to the location of its top-level declaration,
// in the current buffer or a sibling .mh file.
func findDeclaration(path, text, name string) (location, bool) {
	file, src, off, _, ok := locateDeclaration(path, text, name)
	if !ok {
		return location{}, false
	}
	return location{URI: pathToURI(file), Range: identRange(src, off, name)}, true
}

// findMember resolves `receiver.member` to the member's own declaration
// inside receiver's body: for a `tool`, the method line `member(`; for an
// `enum`, the variant token `member`. For every other kind (an agent's
// `.run`, a memory's `.get`/`.set`, an extension's methods) the member is a
// runtime built-in with no source to point at, so the jump lands on the
// receiver's declaration instead — still useful, and it follows imports so
// it opens the right file. The same fallback covers a `tool`/`enum` whose
// member can't be located textually.
func findMember(path, text, receiver, member string) (location, bool) {
	file, src, nameOff, kind, ok := locateDeclaration(path, text, receiver)
	if !ok {
		return location{}, false
	}
	if kind == symTool || kind == symEnum {
		if bodyStart, bodyEnd, ok := blockBounds(src, nameOff); ok {
			var re *regexp.Regexp
			if kind == symTool {
				re = regexp.MustCompile(`(?m)^[ \t]*(` + regexp.QuoteMeta(member) + `)[ \t]*\(`)
			} else {
				re = regexp.MustCompile(`\b(` + regexp.QuoteMeta(member) + `)\b`)
			}
			if m := re.FindStringSubmatchIndex(src[bodyStart:bodyEnd]); m != nil {
				return location{URI: pathToURI(file), Range: identRange(src, bodyStart+m[2], member)}, true
			}
		}
	}
	return location{URI: pathToURI(file), Range: identRange(src, nameOff, receiver)}, true
}

// maxImportHops bounds how many `import` edges locateDeclaration will follow
// before giving up — enough for a name re-exported through a couple of
// barrel files, without risking a pathological chain.
const maxImportHops = 4

// locateDeclaration finds where name is declared, in order of preference:
// the current buffer (path/text); the file an `import { name } from "..."`
// in that buffer points at (following `X as name` aliases, and re-exports up
// to maxImportHops deep); finally a flat scan of every other .mh file one
// level deep in path's directory. It returns the containing file's path and
// full text, the byte offset of the declared name within that text, and the
// declaration's symbolKind.
func locateDeclaration(path, text, name string) (file, src string, nameOff int, kind symbolKind, ok bool) {
	return locateDeclarationHop(path, text, name, 0, map[string]bool{})
}

func locateDeclarationHop(path, text, name string, hop int, seen map[string]bool) (file, src string, nameOff int, kind symbolKind, ok bool) {
	seen[path] = true

	if off, k, found := declMatch(text, name); found {
		return path, text, off, k, true
	}

	// Follow an import that names it (or aliases it).
	if hop < maxImportHops {
		if tgtPath, declName, found := importSource(text, path, name); found && !seen[tgtPath] {
			if b, err := os.ReadFile(tgtPath); err == nil {
				tgt := string(b)
				if off, k, ok := declMatch(tgt, declName); ok {
					return tgtPath, tgt, off, k, true
				}
				if f, s, o, k, ok := locateDeclarationHop(tgtPath, tgt, declName, hop+1, seen); ok {
					return f, s, o, k, true
				}
			}
		}
	}

	// Flat "everything in the same directory is in scope" fallback.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return "", "", 0, 0, false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mh") {
			continue
		}
		full := filepath.Join(filepath.Dir(path), e.Name())
		if seen[full] {
			continue
		}
		b, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		if off, k, found := declMatch(string(b), name); found {
			return full, string(b), off, k, true
		}
	}
	return "", "", 0, 0, false
}

// importRe captures one `import { A, B as C } from "path"` statement: the
// brace-delimited item list (group 1) and the module path (group 2).
var importRe = regexp.MustCompile(`(?m)^[ \t]*import[ \t]*\{([^}]*)\}[ \t]*from[ \t]+"([^"]*)"`)

// importSource scans src's import statements for one that binds `name`
// locally and returns the referenced file (resolved relative to fromPath's
// directory) together with the name as it is declared in that file — an
// `X as name` item maps back to X.
func importSource(src, fromPath, name string) (path, declName string, ok bool) {
	for _, m := range importRe.FindAllStringSubmatch(src, -1) {
		for _, item := range strings.Split(m[1], ",") {
			fields := strings.Fields(item)
			orig, local := "", ""
			switch {
			case len(fields) == 1:
				orig, local = fields[0], fields[0]
			case len(fields) == 3 && fields[1] == "as":
				orig, local = fields[0], fields[2]
			default:
				continue
			}
			if local != name {
				continue
			}
			rel := m[2]
			p := rel
			if !filepath.IsAbs(p) {
				p = filepath.Join(filepath.Dir(fromPath), rel)
			}
			return p, orig, true
		}
	}
	return "", "", false
}

// declMatch scans src for a top-level declaration of name and returns the
// byte offset of the name token plus what kind of declaration introduced it.
func declMatch(src, name string) (int, symbolKind, bool) {
	for _, m := range declLocRe.FindAllStringSubmatchIndex(src, -1) {
		if src[m[4]:m[5]] == name {
			return m[4], declKind(src[m[2]:m[3]]), true
		}
	}
	for _, m := range extDeclLocRe.FindAllStringSubmatchIndex(src, -1) {
		if src[m[2]:m[3]] == name {
			return m[2], symExtension, true
		}
	}
	return 0, 0, false
}

func declKind(keyword string) symbolKind {
	switch keyword {
	case "agent":
		return symAgent
	case "memory":
		return symMemory
	case "tool":
		return symTool
	case "prompt":
		return symPrompt
	case "pipeline", "workflow":
		return symPipeline
	case "enum":
		return symEnum
	default: // "type"
		return symType
	}
}

// blockBounds finds the `{ ... }` block that opens after the offset from and
// returns the byte range between (not including) its braces, brace-depth
// matched and string-literal aware like symbols.go's extractBlock.
func blockBounds(src string, from int) (start, end int, ok bool) {
	open := strings.IndexByte(src[from:], '{')
	if open < 0 {
		return 0, 0, false
	}
	open += from
	depth := 0
	inString := false
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '"':
			if i == 0 || src[i-1] != '\\' {
				inString = !inString
			}
		case '{':
			if !inString {
				depth++
			}
		case '}':
			if !inString {
				depth--
				if depth == 0 {
					return open + 1, i, true
				}
			}
		}
	}
	return 0, 0, false
}

// fromPathRe captures the quoted path of a `from "..."` clause (shared by
// `import` and `prompt ... from`).
var fromPathRe = regexp.MustCompile(`from[ \t]+"([^"]*)"`)

// importPathAt reports the path string of a `from "..."` clause on line when
// ch (a byte offset into line) falls within its quotes.
func importPathAt(line string, ch int) (string, bool) {
	m := fromPathRe.FindStringSubmatchIndex(line)
	if m == nil {
		return "", false
	}
	if ch >= m[2] && ch <= m[3] {
		return line[m[2]:m[3]], true
	}
	return "", false
}

// resolveRelativePath resolves target (relative to curPath's directory, or
// absolute) to an existing file, returning "" when it doesn't resolve.
func resolveRelativePath(curPath, target string) string {
	if target == "" {
		return ""
	}
	p := target
	if !filepath.IsAbs(p) {
		p = filepath.Join(filepath.Dir(curPath), target)
	}
	if info, err := os.Stat(p); err != nil || info.IsDir() {
		return ""
	}
	return p
}

// identAt returns the identifier that covers or ends at the byte offset ch on
// line, together with its start offset. ok is false when ch isn't on an
// identifier.
func identAt(line string, ch int) (word string, start int, ok bool) {
	if ch < 0 {
		ch = 0
	}
	if ch > len(line) {
		ch = len(line)
	}
	start = ch
	for start > 0 && isIdentByte(line[start-1]) {
		start--
	}
	end := ch
	for end < len(line) && isIdentByte(line[end]) {
		end++
	}
	if start == end {
		return "", 0, false
	}
	return line[start:end], start, true
}

// identEndingAt returns the identifier immediately to the left of byte offset
// end on line (end typically points at the "." of a member access).
func identEndingAt(line string, end int) (string, bool) {
	i := end
	for i > 0 && isIdentByte(line[i-1]) {
		i--
	}
	if i == end {
		return "", false
	}
	return line[i:end], true
}

func isIdentByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// lineAt returns line n of text (0-based), or "" when out of range.
func lineAt(text string, n int) string {
	if n < 0 {
		return ""
	}
	lines := strings.Split(text, "\n")
	if n >= len(lines) {
		return ""
	}
	return lines[n]
}

// identRange builds the LSP range covering the len(name) bytes of src that
// start at byte offset off.
func identRange(src string, off int, name string) rangeT {
	return rangeT{Start: offsetToPos(src, off), End: offsetToPos(src, off+len(name))}
}

// offsetToPos converts a byte offset into src to an LSP line/character
// position. Character is a byte count on the line, consistent with the
// ASCII/BMP-only simplification the rest of this package makes.
func offsetToPos(src string, off int) position {
	if off < 0 {
		off = 0
	}
	if off > len(src) {
		off = len(src)
	}
	head := src[:off]
	return position{
		Line:      strings.Count(head, "\n"),
		Character: off - (strings.LastIndexByte(head, '\n') + 1),
	}
}

// pathToURI is uriToPath's inverse: a plain filesystem path to a file:// URI.
func pathToURI(p string) string {
	u := url.URL{Scheme: "file", Path: p}
	return u.String()
}
