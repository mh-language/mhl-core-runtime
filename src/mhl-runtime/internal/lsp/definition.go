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
// cache, when non-nil, batches per-file state for the current
// references/codeLens scan (see refCache) so resolving each identifier's
// declaration reuses already-loaded file content and each candidate file's
// declaration index instead of re-reading and re-scanning it from scratch.
// A plain textDocument/definition lookup (one identifier, called once) has
// no such cache and passes nil, falling back to disk I/O and a one-shot
// index build as usual.
func definitionAt(path, text string, pos position, cache *refCache) []location {
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
			if loc, found := findMember(path, text, receiver, word, cache); found {
				return []location{loc}
			}
		}
	}

	// The cursor sits on a nested member's own declaration (a tool/
	// extensible method or an enum variant): declLocRe only ever captures
	// the enclosing tool/extensible/enum's own name, never the member's, so
	// the top-level lookup below would find nothing here even though a
	// `Receiver.word` reference elsewhere resolves to this exact spot.
	// Recognize it directly so "find references"/"go to definition" work
	// when invoked right on the declaration, not only on a call site.
	off := posToOffset(text, position{Line: pos.Line, Character: start})
	if cache.memberIndex(path, text)[off] {
		return []location{{URI: pathToURI(path), Range: identRange(text, off, word)}}
	}

	if loc, found := findDeclaration(path, text, word, cache); found {
		return []location{loc}
	}
	return nil
}

// declLocRe matches a top-level `<keyword> <Name>` declaration line,
// capturing the keyword (group 1) and the declared name (group 2) — this
// also covers `extensible <kind> { ... }`, whose single identifier is the
// same one-name-after-keyword shape. It mirrors symbols.go's declRe but is
// anchored for FindAllStringSubmatchIndex so the name's byte offset is
// recoverable. extDeclLocRe is its `extension <kind> <Name>` counterpart
// (two identifiers), capturing the name in group 1.
var (
	declLocRe    = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?(?:loop[ \t]+)?(agent|router|memory|tool|prompt|pipeline|workflow|type|enum|extensible)[ \t]+([A-Za-z_][A-Za-z0-9_]*)`)
	extDeclLocRe = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?extension[ \t]+[A-Za-z_][A-Za-z0-9_]*[ \t]+([A-Za-z_][A-Za-z0-9_]*)`)
)

// findDeclaration resolves name to the location of its top-level declaration,
// in the current buffer or a sibling .mh file.
func findDeclaration(path, text, name string, cache *refCache) (location, bool) {
	file, src, off, _, ok := locateDeclaration(path, text, name, cache)
	if !ok {
		return location{}, false
	}
	return location{URI: pathToURI(file), Range: identRange(src, off, name)}, true
}

// findMember resolves `receiver.member` to the member's own declaration
// inside receiver's body: for a `tool` or an `extensible` block, the bare
// method line `member(`; for an `enum`, the variant token `member`. For
// every other kind (an agent's `.run`, a memory's `.get`/`.set`, a usage
// site's `.method(...)` on an `extension <kind> <Name>`) the member is a
// runtime built-in with no source to point at, so the jump lands on the
// receiver's declaration instead — still useful, and it follows imports so
// it opens the right file. The same fallback covers a `tool`/`extensible`/
// `enum` whose member can't be located textually.
func findMember(path, text, receiver, member string, cache *refCache) (location, bool) {
	file, src, nameOff, kind, ok := locateDeclaration(path, text, receiver, cache)
	if !ok {
		return location{}, false
	}
	if kind == symTool || kind == symExtensible || kind == symEnum {
		if bodyStart, bodyEnd, ok := blockBounds(src, nameOff); ok {
			var re *regexp.Regexp
			if kind == symEnum {
				re = regexp.MustCompile(`\b(` + regexp.QuoteMeta(member) + `)\b`)
			} else {
				re = regexp.MustCompile(`(?m)^[ \t]*(` + regexp.QuoteMeta(member) + `)[ \t]*\(`)
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
func locateDeclaration(path, text, name string, cache *refCache) (file, src string, nameOff int, kind symbolKind, ok bool) {
	return locateDeclarationHop(path, text, name, 0, map[string]bool{}, cache)
}

// declEntry is one top-level declaration's position and kind within a
// specific file, as declMatch would compute it — refCache.index memoizes a
// whole file's worth of these at once so looking up many different names
// against the same file costs one regex pass instead of one per name.
type declEntry struct {
	off  int
	kind symbolKind
}

// refCache batches per-file state for one references/codeLens scan (built
// once by referenceFiles' caller — see references.go) or a plain
// textDocument/definition lookup (nil): each candidate file's content, and
// each candidate file's declaration index, built lazily on first use and
// reused after. Without the index, resolving one file's N identifiers each
// independently re-ran declMatch's regex scan over every candidate file's
// full text — on a real project, most identifiers are ordinary variables or
// parameters that aren't a declaration anywhere, so every single one of them
// still probed (and fully rescanned) each candidate file before giving up,
// which is what made references/codeLens pathologically slow on a
// real-sized project. A single goroutine handles one LSP request at a time
// (server.go's message loop), so this needs no locking.
type refCache struct {
	files   map[string]string
	decls   map[string]map[string]declEntry
	members map[string]map[int]bool
}

func newRefCache(files map[string]string) *refCache {
	return &refCache{files: files, decls: map[string]map[string]declEntry{}}
}

// memberIndex returns the set of byte offsets, within path/text, where a
// nested tool/extensible method or enum variant is declared — the same
// declarations declarationLocations (references.go, what codeLens itself
// renders from) marks, reused here so a bare identifier occurrence that IS
// one of these declarations resolves to itself (see definitionAt). Built
// once per file and memoized like index.
func (c *refCache) memberIndex(path, text string) map[int]bool {
	if c == nil {
		return buildMemberOffsets(path, text)
	}
	if idx, ok := c.members[path]; ok {
		return idx
	}
	idx := buildMemberOffsets(path, text)
	if c.members == nil {
		c.members = map[string]map[int]bool{}
	}
	c.members[path] = idx
	return idx
}

func buildMemberOffsets(path, text string) map[int]bool {
	offs := map[int]bool{}
	for _, loc := range declarationLocations(path, text) {
		offs[posToOffset(text, loc.Range.Start)] = true
	}
	return offs
}

// readFile reads path's content, preferring an already-loaded file (a
// references/codeLens scan's own files, or a prior read this call already
// memoized) over touching disk. c may be nil, in which case this always
// reads through without memoizing.
func (c *refCache) readFile(path string) (string, bool) {
	if c != nil {
		if src, ok := c.files[path]; ok {
			return src, true
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// siblings lists dir's .mh files. When c and c.files are set (a
// references/codeLens scan already covering every file under some root) it
// reads the file set's own keys instead of re-listing the directory from
// disk, since a single such scan calls this once per identifier that isn't
// declared locally or reached by import.
func (c *refCache) siblings(dir string) []string {
	if c != nil && c.files != nil {
		var out []string
		for p := range c.files {
			if filepath.Dir(p) == dir {
				out = append(out, p)
			}
		}
		return out
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mh") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out
}

// index returns path's declared-name → declEntry map, building it once via
// buildDeclIndex and reusing it for every later lookup against path within
// this same cache's lifetime (one references/codeLens scan, or nil for a
// single one-shot lookup that gains nothing from memoizing).
func (c *refCache) index(path, text string) map[string]declEntry {
	if c == nil {
		return buildDeclIndex(text)
	}
	if idx, ok := c.decls[path]; ok {
		return idx
	}
	idx := buildDeclIndex(text)
	c.decls[path] = idx
	return idx
}

func locateDeclarationHop(path, text, name string, hop int, seen map[string]bool, cache *refCache) (file, src string, nameOff int, kind symbolKind, ok bool) {
	seen[path] = true

	if e, found := cache.index(path, text)[name]; found {
		return path, text, e.off, e.kind, true
	}

	// Follow an import that names it (or aliases it).
	if hop < maxImportHops {
		if tgtPath, declName, found := importSource(text, path, name); found && !seen[tgtPath] {
			if tgt, ok := cache.readFile(tgtPath); ok {
				if e, ok := cache.index(tgtPath, tgt)[declName]; ok {
					return tgtPath, tgt, e.off, e.kind, true
				}
				if f, s, o, k, ok := locateDeclarationHop(tgtPath, tgt, declName, hop+1, seen, cache); ok {
					return f, s, o, k, true
				}
			}
		}
	}

	// Flat "everything in the same directory is in scope" fallback.
	for _, full := range cache.siblings(filepath.Dir(path)) {
		if seen[full] {
			continue
		}
		src, ok := cache.readFile(full)
		if !ok {
			continue
		}
		if e, found := cache.index(full, src)[name]; found {
			return full, src, e.off, e.kind, true
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

// buildDeclIndex scans src once for every top-level declaration and returns
// a declared-name → declEntry map — the batch form of what used to be
// declMatch's per-name regex scan, keyed so refCache.index can memoize it
// per file. Matches declMatch's precedence: the first declLocRe occurrence
// of a name wins, and an extDeclLocRe ("extension <kind> <Name>") entry is
// only added when that name has no declLocRe declaration at all.
func buildDeclIndex(src string) map[string]declEntry {
	idx := map[string]declEntry{}
	for _, m := range declLocRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[4]:m[5]]
		if _, exists := idx[name]; exists {
			continue
		}
		idx[name] = declEntry{off: m[4], kind: declKind(src[m[2]:m[3]])}
	}
	for _, m := range extDeclLocRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if _, exists := idx[name]; exists {
			continue
		}
		idx[name] = declEntry{off: m[2], kind: symExtension}
	}
	return idx
}

func declKind(keyword string) symbolKind {
	switch keyword {
	case "agent":
		return symAgent
	case "router":
		return symRouter
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
	case "extensible":
		return symExtensible
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

// posToOffset is offsetToPos's inverse.
func posToOffset(src string, pos position) int {
	lines := strings.Split(src, "\n")
	if pos.Line < 0 {
		return 0
	}
	if pos.Line >= len(lines) {
		return len(src)
	}
	off := 0
	for i := 0; i < pos.Line; i++ {
		off += len(lines[i]) + 1
	}
	ch := pos.Character
	if ch < 0 {
		ch = 0
	}
	if ch > len(lines[pos.Line]) {
		ch = len(lines[pos.Line])
	}
	return off + ch
}

// pathToURI is uriToPath's inverse: a plain filesystem path to a file://
// URI. filepath.ToSlash normalizes Windows backslashes, and a drive-letter
// path ("C:\...") needs a leading "/" ahead of the drive letter — the
// "file:///C:/..." shape every LSP client (and uriToPath's decoding above)
// expects; without it url.URL treats "C:" as the URI's host instead of the
// start of the path.
func pathToURI(p string) string {
	p = filepath.ToSlash(p)
	if len(p) >= 2 && p[1] == ':' && isASCIILetter(p[0]) {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	return u.String()
}
