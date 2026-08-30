package lsp

import (
	"regexp"
	"strings"
)

// keywords is every reserved word in the MHL grammar (internal/lang/ast),
// offered as a plain keyword completion whenever the cursor isn't in a
// member-access position.
var keywords = []string{
	"agent", "memory", "tool", "prompt", "pipeline", "workflow", "extension", "loop",
	"use", "from", "as", "export", "input", "step", "test", "describe",
	"var", "const", "type", "enum", "match", "if", "else", "while", "for", "in", "try", "catch", "finally",
	"return", "break", "goto", "spawn", "wait", "parallel", "true", "false", "null",
}

// memberAccessRe matches an in-progress "name.partial" at the very end of
// the text before the cursor, capturing the target identifier. Used to
// switch completion from "everything" to "target's members only".
var memberAccessRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\.[A-Za-z0-9_]*$`)

// typeAnnotationRe matches an in-progress "name: partialType" at the very
// end of the text before the cursor — the shape of a `input name: ` or tool
// method `param: ` type annotation (see internal/lang/types' vocabulary).
// isTypeAnnotationPosition additionally
// restricts where this fires so it never fires on an ordinary `key: value`
// property (e.g. `agent { command: }`).
var typeAnnotationRe = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\s*:\s*[A-Za-z_]*$`)

// typeKeywords is internal/lang/types' declarable vocabulary, offered
// whenever the cursor sits in a recognized type-annotation position.
var typeKeywords = []string{"string", "number", "bool", "array", "object", "any"}

// isTypeAnnotationPosition is a pragmatic heuristic, not a real parse —
// consistent with blockStack/classifyHeader's own best-effort approach
// elsewhere in this package. It recognizes two shapes: a pipeline `input
// name: ` line (line itself starts with "input "), and a tool/prompt method
// parameter list (`name(param: ` — the enclosing block is blockOther, and
// the line has an unclosed "(" before the match).
func isTypeAnnotationPosition(linePrefix, text string, pos position) bool {
	if strings.HasPrefix(strings.TrimSpace(linePrefix), "input ") {
		return true
	}
	stack := blockStack(textUpToPosition(text, pos))
	if len(stack) == 0 || stack[len(stack)-1].Kind != blockOther {
		return false
	}
	return strings.Count(linePrefix, "(") > strings.Count(linePrefix, ")")
}

// completionAt computes the completion list for path/text at pos, following
// three modes: member completion right after "target." (only target's own
// methods), property-name completion when the cursor sits directly inside a
// recognized declaration body or nested config object (an `agent { }`, a
// `pipeline { }`/`loop pipeline { }`, or one of their own nested `checkpoint
// { }`/`repeat { }`/`retry { }`/`cache { }`/`rate_limit { }` blocks — see
// blockStack/propertyItemsFor), appended to the general list rather than
// replacing it since the block classifier is best-effort, not authoritative
// — or general completion everywhere else (keywords + every symbol in
// scope).
func completionAt(path, text string, pos position) []completionItem {
	linePrefix := textBeforePosition(text, pos)

	if m := memberAccessRe.FindStringSubmatch(linePrefix); m != nil {
		target := m[1]
		for _, s := range documentSymbols(path, text) {
			if s.Name == target {
				return methodItems(s)
			}
		}
		return nil
	}

	if typeAnnotationRe.MatchString(linePrefix) && isTypeAnnotationPosition(linePrefix, text, pos) {
		items := make([]completionItem, 0, len(typeKeywords))
		for _, kw := range typeKeywords {
			items = append(items, completionItem{Label: kw, Kind: kindKeyword})
		}
		// A `type X = ...` alias or an `enum` name is usable anywhere a
		// builtin type keyword is.
		for _, s := range documentSymbols(path, text) {
			if s.Kind == symType || s.Kind == symEnum {
				items = append(items, completionItem{Label: s.Name, Kind: kindKeyword, Detail: s.Kind.label()})
			}
		}
		return items
	}

	items := make([]completionItem, 0, len(keywords))
	for _, kw := range keywords {
		items = append(items, completionItem{Label: kw, Kind: kindKeyword})
	}
	for _, s := range documentSymbols(path, text) {
		items = append(items, completionItem{
			Label:  s.Name,
			Kind:   symbolItemKind(s.Kind),
			Detail: s.Kind.label(),
		})
	}
	items = append(items, propertyItemsFor(blockStack(textUpToPosition(text, pos)))...)
	return items
}

func methodItems(s symbol) []completionItem {
	items := make([]completionItem, 0, len(s.Methods))
	for _, m := range s.Methods {
		item := completionItem{
			Label:      m,
			Kind:       kindMethod,
			Detail:     s.Kind.label() + " method",
			InsertText: m + "(",
		}
		// Replace the generic "<kind> method" detail with the real
		// signature, and attach its parameter doc, whenever one is known
		// (every native op, collection method, and declared-construct
		// method — see signatures.go).
		if sg, ok := signatureForMethod(s, m); ok {
			item.Detail = sg.Label
			if sg.Doc != "" {
				item.Documentation = &markupContent{Kind: "markdown", Value: sg.Doc}
			}
		}
		items = append(items, item)
	}
	return items
}

func symbolItemKind(k symbolKind) int {
	switch k {
	case symAgent, symTool, symMemory:
		return kindClass
	case symPrompt, symPipeline, symExtension:
		return kindProperty
	case symNative:
		return kindModule
	case symType, symEnum:
		return kindKeyword
	default:
		return kindText
	}
}

// textBeforePosition returns the text of line pos.Line up to (not
// including) character pos.Character — the slice completion logic matches
// trigger patterns like "name." against.
func textBeforePosition(text string, pos position) string {
	lines := strings.Split(text, "\n")
	if pos.Line < 0 || pos.Line >= len(lines) {
		return ""
	}
	line := lines[pos.Line]
	// Character is a UTF-16 code-unit offset per the LSP spec; MHL source is
	// expected to be ASCII/BMP-only in practice, so treating it as a byte
	// offset here is a deliberate simplification, not a spec-correct decode.
	if pos.Character < 0 {
		return ""
	}
	if pos.Character > len(line) {
		return line
	}
	return line[:pos.Character]
}
