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
func referencesAt(path, text string, pos position, includeDeclaration bool, openDocs map[string]string) []location {
	targets := definitionAt(path, text, pos)
	if len(targets) == 0 {
		return []location{}
	}
	return referencesTo(targets[0], path, text, includeDeclaration, openDocs)
}

func referencesTo(target location, path, text string, includeDeclaration bool, openDocs map[string]string) []location {
	files := referenceFiles(path, text, openDocs)
	var out []location
	for file, src := range files {
		for _, off := range identifierOffsets(src) {
			pos := offsetToPos(src, off)
			defs := definitionAt(file, src, pos)
			if len(defs) == 0 || !sameLocation(defs[0], target) {
				continue
			}
			// Built-in members such as Agent.run deliberately navigate to the
			// receiver declaration. Count Agent once, not both Agent and run.
			line := lineAt(src, pos.Line)
			if pos.Character > 0 && line[pos.Character-1] == '.' {
				if receiver, ok := identEndingAt(line, pos.Character-1); ok {
					receiverPos := position{Line: pos.Line, Character: pos.Character - 1 - len(receiver)}
					if receiverDefs := definitionAt(file, src, receiverPos); len(receiverDefs) > 0 && sameLocation(receiverDefs[0], target) {
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
func referenceFiles(path, text string, openDocs map[string]string) map[string]string {
	files := map[string]string{filepath.Clean(path): text}
	root := referenceRoot(path)
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".mh") {
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

// referenceRoot uses the nearest project marker when present, otherwise the
// current file's directory. This lets a declaration in a subdirectory see
// callers elsewhere in the same workspace without scanning unrelated trees.
func referenceRoot(path string) string {
	dir := filepath.Dir(path)
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
func codeLenses(path, text string, openDocs map[string]string) []codeLens {
	decls := declarationLocations(path, text)
	counts := referenceCounts(path, text, openDocs)
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

func referenceCounts(path, text string, openDocs map[string]string) map[string]int {
	counts := map[string]int{}
	for file, src := range referenceFiles(path, text, openDocs) {
		for _, off := range identifierOffsets(src) {
			pos := offsetToPos(src, off)
			defs := definitionAt(file, src, pos)
			if len(defs) == 0 {
				continue
			}
			line := lineAt(src, pos.Line)
			if pos.Character > 0 && line[pos.Character-1] == '.' {
				if receiver, ok := identEndingAt(line, pos.Character-1); ok {
					receiverPos := position{Line: pos.Line, Character: pos.Character - 1 - len(receiver)}
					if receiverDefs := definitionAt(file, src, receiverPos); len(receiverDefs) > 0 && sameLocation(receiverDefs[0], defs[0]) {
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
				add(memberOff, member)
			}
		}
	}
	for _, m := range extDeclLocRe.FindAllStringSubmatchIndex(text, -1) {
		add(m[2], text[m[2]:m[3]])
	}
	return out
}
