package lsp

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// referencesAt resolves the symbol under the cursor, then returns only the
// identifier occurrences whose own definition resolves to that exact symbol.
// This deliberately reuses definitionAt so imports, aliases and member access
// have identical semantics in Go to Definition, Find References and CodeLens.
func referencesAt(path, text string, pos position, includeDeclaration bool, openDocs map[string]string, workspaceRoot string) []location {
	cache := newRefCache(referenceFiles(path, text, openDocs, workspaceRoot))
	targets := definitionAt(path, text, pos, cache)
	if len(targets) == 0 {
		return []location{}
	}
	return referencesTo(targets[0], cache, includeDeclaration)
}

// referencesTo scans every file in cache (a references/codeLens-scoped file
// set — see referenceFiles) for identifier occurrences resolving to target,
// reusing that same cache for definitionAt's lookups so resolving each of a
// real file's hundreds of identifiers doesn't re-read and re-scan the
// project's other files once per identifier.
func referencesTo(target location, cache *refCache, includeDeclaration bool) []location {
	var out []location
	for file, src := range cache.files {
		for _, off := range identifierOffsets(src) {
			pos := offsetToPos(src, off)
			defs := definitionAt(file, src, pos, cache)
			if len(defs) == 0 || !sameLocation(defs[0], target) {
				continue
			}
			// Built-in members such as Agent.run deliberately navigate to the
			// receiver declaration. Count Agent once, not both Agent and run.
			line := lineAt(src, pos.Line)
			if pos.Character > 0 && line[pos.Character-1] == '.' {
				if receiver, ok := identEndingAt(line, pos.Character-1); ok {
					receiverPos := position{Line: pos.Line, Character: pos.Character - 1 - len(receiver)}
					if receiverDefs := definitionAt(file, src, receiverPos, cache); len(receiverDefs) > 0 && sameLocation(receiverDefs[0], target) {
						continue
					}
				}
			}
			word, _, ok := identAt(line, pos.Character)
			if !ok {
				continue
			}
			loc := location{URI: pathToURI(file), Range: identRange(src, off, word)}
			if !includeDeclaration && sameLocation(loc, target) {
				continue
			}
			out = append(out, loc)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].URI != out[j].URI {
			return out[i].URI < out[j].URI
		}
		if out[i].Range.Start.Line != out[j].Range.Start.Line {
			return out[i].Range.Start.Line < out[j].Range.Start.Line
		}
		return out[i].Range.Start.Character < out[j].Range.Start.Character
	})
	return out
}

func sameLocation(a, b location) bool {
	return a.URI == b.URI && a.Range.Start == b.Range.Start && a.Range.End == b.Range.End
}

var identifierRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

func identifierOffsets(src string) []int {
	var offsets []int
	inString, inComment, escaped := false, false, false
	interpolationDepth := 0
	for i := 0; i < len(src); {
		if inComment {
			if src[i] == '\n' {
				inComment = false
			}
			i++
			continue
		}
		if inString && interpolationDepth == 0 {
			if escaped {
				escaped = false
				i++
				continue
			}
			if src[i] == '\\' {
				escaped = true
				i++
				continue
			}
			if i+1 < len(src) && src[i] == '$' && src[i+1] == '{' {
				interpolationDepth = 1
				i += 2
				continue
			}
			if src[i] == '"' {
				inString = false
			}
			i++
			continue
		}
		if i+1 < len(src) && src[i] == '/' && src[i+1] == '/' {
			inComment = true
			i += 2
			continue
		}
		if src[i] == '"' && interpolationDepth == 0 {
			inString = true
			i++
			continue
		}
		if interpolationDepth > 0 {
			switch src[i] {
			case '{':
				interpolationDepth++
			case '}':
				interpolationDepth--
				i++
				continue
			}
		}
		if isIdentByte(src[i]) && !(src[i] >= '0' && src[i] <= '9') {
			start := i
			for i < len(src) && isIdentByte(src[i]) {
				i++
			}
			offsets = append(offsets, start)
			continue
		}
		i++
	}
	return offsets
}

// referenceFiles scans the current directory recursively. Open buffers win
// over their on-disk versions, so counts update immediately while typing.
func referenceFiles(path, text string, openDocs map[string]string, workspaceRoot string) map[string]string {
	files := map[string]string{filepath.Clean(path): text}
	root := referenceRoot(path, workspaceRoot)
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && skipReferenceDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".mh") {
			return nil
		}
		p = filepath.Clean(p)
		if _, exists := files[p]; exists {
			return nil
		}
		if b, err := os.ReadFile(p); err == nil {
			files[p] = string(b)
		}
		return nil
	})
	for uri, src := range openDocs {
		p := filepath.Clean(uriToPath(uri))
		if p == filepath.Clean(path) || isWithin(root, p) {
			files[p] = src
		}
	}
	return files
}

// skipReferenceDir reports directories referenceFiles' walk never has a
// reason to enter: dependency trees (node_modules is the one virtually every
// ecosystem uses) and dot-directories (.git, editor/tool state like .vscode
// or .mhl) — never where an MHL project keeps its own .mh sources. Skipping
// them via filepath.SkipDir, rather than filtering by extension after
// listing every entry, matters once workspaceRoot (see referenceRoot) is a
// real project root: without it, a references/codeLens request walks the
// entire node_modules tree and the whole .git object store on every call,
// which is slow enough on a real-sized project to make the LSP feel like
// it's hanging or looping.
func skipReferenceDir(name string) bool {
	return name == "node_modules" || strings.HasPrefix(name, ".")
}

// referenceRoot prefers the editor-reported workspace root (workspaceRoot,
// from "initialize"'s rootUri/workspaceFolders — see server.go) whenever
// path is inside it, since that's the project boundary the client actually
// opened. Absent that (a bare "mhl lsp" session, or a file outside the
// reported workspace), it falls back to the nearest ancestor project marker,
// and finally the current file's own directory. This lets a declaration in
// a subdirectory see callers elsewhere in the same project without scanning
// unrelated trees — including a project with no ".git"/"go.mod" at all,
// where the marker fallback alone would wrongly stop at the file's own
// directory and miss a caller one level up.
func referenceRoot(path, workspaceRoot string) string {
	dir := filepath.Dir(path)
	if workspaceRoot != "" && isWithin(workspaceRoot, path) {
		return workspaceRoot
	}
	for cur := dir; ; cur = filepath.Dir(cur) {
		for _, marker := range []string{".git", "go.mod"} {
			if _, err := os.Stat(filepath.Join(cur, marker)); err == nil {
				return cur
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return dir
		}
	}
}

func isWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// codeLenses emits a reference count above top-level declarations and above
// source-declared tool/extensible methods and enum variants.
func codeLenses(path, text string, openDocs map[string]string, workspaceRoot string) []codeLens {
	decls := declarationLocations(path, text)
	counts := referenceCounts(path, text, openDocs, workspaceRoot)
	lenses := make([]codeLens, 0, len(decls))
	for _, decl := range decls {
		count := counts[locationKey(decl)]
		title := "0 references"
		if count == 1 {
			title = "1 reference"
		} else if count != 0 {
			title = fmt.Sprintf("%d references", count)
		}
		lenses = append(lenses, codeLens{
			Range:   decl.Range,
			Command: command{Title: title, Command: "mhl.showReferences", Arguments: []any{pathToURI(path), decl.Range.Start}},
		})
	}
	return lenses
}

func referenceCounts(path, text string, openDocs map[string]string, workspaceRoot string) map[string]int {
	counts := map[string]int{}
	cache := newRefCache(referenceFiles(path, text, openDocs, workspaceRoot))
	for file, src := range cache.files {
		for _, off := range identifierOffsets(src) {
			pos := offsetToPos(src, off)
			defs := definitionAt(file, src, pos, cache)
			if len(defs) == 0 {
				continue
			}
			line := lineAt(src, pos.Line)
			if pos.Character > 0 && line[pos.Character-1] == '.' {
				if receiver, ok := identEndingAt(line, pos.Character-1); ok {
					receiverPos := position{Line: pos.Line, Character: pos.Character - 1 - len(receiver)}
					if receiverDefs := definitionAt(file, src, receiverPos, cache); len(receiverDefs) > 0 && sameLocation(receiverDefs[0], defs[0]) {
						continue
					}
				}
			}
			word, _, ok := identAt(line, pos.Character)
			if !ok {
				continue
			}
			occurrence := location{URI: pathToURI(file), Range: identRange(src, off, word)}
			if !sameLocation(occurrence, defs[0]) {
				counts[locationKey(defs[0])]++
			}
		}
	}
	return counts
}

func locationKey(loc location) string {
	return fmt.Sprintf("%s:%d:%d:%d:%d", loc.URI, loc.Range.Start.Line, loc.Range.Start.Character, loc.Range.End.Line, loc.Range.End.Character)
}

// reservedForMemberDecl is completion.go's own keyword list, reused here to
// reject a false match: the tool/extensible member regex below only checks
// "identifier at line start followed by (", a shape a control-flow
// statement like `if (...)`, `while (...)`, `for (...)` or `return (...)`
// matches just as well as a real `name(params) -> {` method declaration —
// this cache never sees the block nesting that would otherwise tell the two
// apart.
var reservedForMemberDecl = func() map[string]bool {
	m := make(map[string]bool, len(keywords))
	for _, k := range keywords {
		m[k] = true
	}
	return m
}()

func declarationLocations(path, text string) []location {
	var out []location
	add := func(off int, name string) {
		out = append(out, location{URI: pathToURI(path), Range: identRange(text, off, name)})
	}
	for _, m := range declLocRe.FindAllStringSubmatchIndex(text, -1) {
		name, off := text[m[4]:m[5]], m[4]
		add(off, name)
		kind := declKind(text[m[2]:m[3]])
		if kind != symTool && kind != symExtensible && kind != symEnum {
			continue
		}
		if start, end, ok := blockBounds(text, off); ok {
			var re *regexp.Regexp
			if kind == symEnum {
				re = identifierRe
			} else {
				re = regexp.MustCompile(`(?m)^[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]*\(`)
			}
			for _, mm := range re.FindAllStringSubmatchIndex(text[start:end], -1) {
				idx := mm[0]
				if len(mm) >= 4 {
					idx = mm[2]
				}
				memberOff := start + idx
				member := identifierRe.FindString(text[memberOff:])
				if kind != symEnum && reservedForMemberDecl[member] {
					continue
				}
				add(memberOff, member)
			}
		}
	}
	for _, m := range extDeclLocRe.FindAllStringSubmatchIndex(text, -1) {
		add(m[2], text[m[2]:m[3]])
	}
	return out
}
