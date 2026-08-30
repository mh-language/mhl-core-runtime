package lsp

import (
	"regexp"
	"strings"
)

// blockKind classifies one open, unclosed "{" found while scanning a buffer
// up to the cursor — enough to tell property-position completion.go what
// declaration (or nested config object) the cursor is currently inside.
// blockOther covers every brace this classifier doesn't recognize (an `if
// (...) {`, a `try {`, a plain object literal in a step's `var x = {...}`,
// ...) — it still occupies a stack slot so depth tracking stays correct,
// it just contributes no completion context.
type blockKind int

const (
	blockOther blockKind = iota
	blockAgent
	blockPipeline
	blockLoopPipeline
	blockCheckpoint
	blockSpawn
	blockParallel
	blockRepeat
	blockContext
	blockRetry
	blockCache
	blockRateLimit
	blockExtension // an `extension <kind> <Name>` body; ExtKind carries the kind
)

// blockRef is one classified open "{": its kind plus, for blockExtension, the
// extension kind ("mcp", "a2a", ...) so property completion can read that
// kind's DeclarationSpec.
type blockRef struct {
	Kind    blockKind
	ExtKind string
}

// headerRe pairs a blockKind with the regex that recognizes the token(s)
// immediately preceding a "{" that opens it — each anchored at $ so it only
// has to match the *tail* of everything scanned since the previous brace
// (see blockStack), regardless of how much unrelated text precedes it.
// Order matters where one pattern is a suffix of another (loop pipeline vs
// pipeline): the more specific one is checked first.
var headerRe = []struct {
	kind blockKind
	re   *regexp.Regexp
}{
	{blockLoopPipeline, regexp.MustCompile(`\bloop\s+(?:pipeline|workflow)\s+\w+\s*$`)},
	{blockPipeline, regexp.MustCompile(`\b(?:pipeline|workflow)\s+\w+\s*$`)},
	{blockAgent, regexp.MustCompile(`\bagent\s+\w*\s*$`)}, // \w* (not \w+): an inline `fallback: [agent { ... }]` literal has no name
	{blockParallel, regexp.MustCompile(`\bparallel\s+\w+\s*$`)},
	{blockCheckpoint, regexp.MustCompile(`\bcheckpoint\s*:\s*$`)},
	{blockSpawn, regexp.MustCompile(`\bspawn\s*:\s*$`)},
	{blockRepeat, regexp.MustCompile(`\brepeat\s*:\s*$`)},
	{blockContext, regexp.MustCompile(`\bcontext\s*:\s*$`)},
	{blockRetry, regexp.MustCompile(`\bretry\s*:\s*$`)},
	{blockCache, regexp.MustCompile(`\bcache\s*:\s*$`)},
	{blockRateLimit, regexp.MustCompile(`\brate_limit\s*:\s*$`)},
}

// extHeaderRe recognises an `extension <kind> <Name>` header and captures the
// kind from the source.
var extHeaderRe = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"", regexp.MustCompile(`\bextension\s+(\w+)\s+\w+\s*$`)},
}

// classifyHeader matches s — the raw text since the previous brace, up to
// (not including) the "{" now being opened — against the header patterns.
func classifyHeader(s string) blockRef {
	for _, h := range extHeaderRe {
		m := h.re.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		kind := h.kind
		if kind == "" && len(m) > 1 {
			kind = m[1]
		}
		return blockRef{Kind: blockExtension, ExtKind: kind}
	}
	for _, h := range headerRe {
		if h.re.MatchString(s) {
			return blockRef{Kind: h.kind}
		}
	}
	return blockRef{Kind: blockOther}
}

// blockStack scans src (already truncated to the cursor by
// textUpToPosition) and returns the classified kind of every currently-open
// "{", outermost first — the last element is what directly encloses the
// cursor, exactly what property-position completion needs. String-literal
// aware (a stray "{"/"}" inside a quoted value never perturbs the count),
// mirroring symbols.go's extractBlock, just walking the whole buffer
// forward instead of one already-located block.
func blockStack(src string) []blockRef {
	var stack []blockRef
	inString := false
	tokenStart := 0
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '"':
			if i == 0 || src[i-1] != '\\' {
				inString = !inString
			}
		case '{':
			if inString {
				continue
			}
			stack = append(stack, classifyHeader(src[tokenStart:i]))
			tokenStart = i + 1
		case '}':
			if inString {
				continue
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			tokenStart = i + 1
		}
	}
	return stack
}

// textUpToPosition returns all of text from its start through (not
// including) pos — textBeforePosition's multi-line counterpart, needed here
// because the "{" that opens the cursor's enclosing block is typically many
// lines above the cursor's own line.
func textUpToPosition(text string, pos position) string {
	lines := strings.Split(text, "\n")
	if pos.Line < 0 {
		return ""
	}
	end := pos.Line
	if end > len(lines) {
		end = len(lines)
	}
	var b strings.Builder
	for i := 0; i < end; i++ {
		b.WriteString(lines[i])
		b.WriteByte('\n')
	}
	if end < len(lines) {
		line := lines[end]
		c := pos.Character
		if c < 0 {
			c = 0
		}
		if c > len(line) {
			c = len(line)
		}
		b.WriteString(line[:c])
	}
	return b.String()
}
