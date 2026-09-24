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
	blockRouter
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
	blockTool      // a `tool <Name>` body; Name carries the tool's declared name

	// blockSessionStartHook/blockSessionEndHook/blockStepStartHook/
	// blockStepEndHook/blockStopFailureHook are a pipeline/workflow lifecycle
	// hook's own lambda body — `session_start: (session) -> { ... }` and its
	// four siblings (ast.PipelineBodyProperties). Name carries the lambda's
	// single parameter as the user actually spelled it (not a fixed
	// "session"/"step"/"failure" — see hookcontext.go), which is what lets
	// completion.go offer `<param>.` field completion for whichever builtin
	// type (SessionContext/StepContext/FailureContext) that hook's
	// PipelineBodyProperty.ParamType names.
	blockSessionStartHook
	blockSessionEndHook
	blockStepStartHook
	blockStepEndHook
	blockStopFailureHook
)

// blockRef is one classified open "{": its kind plus, for blockExtension, the
// extension kind ("mcp", "a2a", ...) so property completion can read that
// kind's DeclarationSpec — and, for blockPipeline/blockLoopPipeline/blockTool,
// the declaration's own name, so self-completion (completion.go) knows which
// pipeline/tool the cursor's `self.` belongs to without re-scanning from the
// document start.
type blockRef struct {
	Kind    blockKind
	ExtKind string
	Name    string
}

// sigRe matches a pipeline/workflow header's optional typed signature,
// `(req: ReviewInput): ReviewOutput`, without capturing it.
const sigRe = `(?:\s*\(\s*\w+\s*:\s*[\w\[\]]+\s*\))?(?:\s*:\s*[\w\[\]]+)?`

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
	// A typed signature — `workflow W(req: In): Out` — may sit between the
	// name and the `{` (sigRe). Only named types (optionally `[]`-suffixed)
	// are recognized there: an inline `{ ... }` shape in the header opens
	// braces of its own, which this brace-counting scan can't tell apart.
	{blockLoopPipeline, regexp.MustCompile(`\b(?:partial\s+)?loop\s+(?:pipeline|workflow)\s+(\w+)` + sigRe + `(?:\s+max\s+\d+)?\s*$`)},
	{blockPipeline, regexp.MustCompile(`\b(?:partial\s+)?(?:pipeline|workflow)\s+(\w+)` + sigRe + `\s*$`)},
	{blockTool, regexp.MustCompile(`\btool\s+(\w+)\s*$`)},
	{blockAgent, regexp.MustCompile(`\bagent\s+\w*\s*$`)}, // \w* (not \w+): an inline `fallback: [agent { ... }]` literal has no name
	{blockRouter, regexp.MustCompile(`\brouter\s+\w+\s*$`)},
	{blockParallel, regexp.MustCompile(`\bparallel\s+\w+\s*$`)},
	{blockCheckpoint, regexp.MustCompile(`\bcheckpoint\s*:\s*$`)},
	{blockSpawn, regexp.MustCompile(`\bspawn\s*:\s*$`)},
	{blockRepeat, regexp.MustCompile(`\brepeat\s*:\s*$`)},
	{blockContext, regexp.MustCompile(`\bcontext\s*:\s*$`)},
	{blockRetry, regexp.MustCompile(`\bretry\s*:\s*$`)},
	{blockCache, regexp.MustCompile(`\bcache\s*:\s*$`)},
	{blockRateLimit, regexp.MustCompile(`\brate_limit\s*:\s*$`)},
	// (?:\s*:\s*\w+)? tolerates an explicit (and unenforced — see
	// PipelineBodyProperty.ParamType's doc comment) `(session: SessionContext)
	// -> ...` type annotation without capturing it: the parameter's own name
	// is always group 1, whichever form was written.
	{blockSessionStartHook, regexp.MustCompile(`\bsession_start\s*:\s*\(\s*(\w+)(?:\s*:\s*\w+)?\s*\)\s*->\s*$`)},
	{blockSessionEndHook, regexp.MustCompile(`\bsession_end\s*:\s*\(\s*(\w+)(?:\s*:\s*\w+)?\s*\)\s*->\s*$`)},
	{blockStepStartHook, regexp.MustCompile(`\bstep_start\s*:\s*\(\s*(\w+)(?:\s*:\s*\w+)?\s*\)\s*->\s*$`)},
	{blockStepEndHook, regexp.MustCompile(`\bstep_end\s*:\s*\(\s*(\w+)(?:\s*:\s*\w+)?\s*\)\s*->\s*$`)},
	{blockStopFailureHook, regexp.MustCompile(`\bstop_failure\s*:\s*\(\s*(\w+)(?:\s*:\s*\w+)?\s*\)\s*->\s*$`)},
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
		m := h.re.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		name := ""
		if len(m) > 1 {
			name = m[1]
		}
		return blockRef{Kind: h.kind, Name: name}
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
