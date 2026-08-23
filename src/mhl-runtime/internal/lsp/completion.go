package lsp

import (
	"regexp"
	"strings"
)

// keywords is every reserved word in the MHL grammar (internal/lang/ast),
// offered as a plain keyword completion whenever the cursor isn't in a
// member-access position.
var keywords = []string{
	"agent", "memory", "tool", "prompt", "skill", "pipeline", "mcp_server", "loop",
	"import", "use", "from", "as", "export", "input", "step", "test", "describe",
	"var", "if", "else", "while", "for", "in", "try", "catch", "finally",
	"return", "break", "goto", "true", "false", "null",
}

// memberAccessRe matches an in-progress "name.partial" at the very end of
// the text before the cursor, capturing the target identifier. Used to
// switch completion from "everything" to "target's members only".
var memberAccessRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\.[A-Za-z0-9_]*$`)

// completionAt computes the completion list for path/text at pos, following
// the same two modes every LSP client expects: member completion right
// after "target." (only target's own methods), or general completion
// everywhere else (keywords + every symbol in scope).
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
	return items
}

func methodItems(s symbol) []completionItem {
	items := make([]completionItem, 0, len(s.Methods))
	for _, m := range s.Methods {
		items = append(items, completionItem{
			Label:      m,
			Kind:       kindMethod,
			Detail:     s.Kind.label() + " method",
			InsertText: m + "(",
		})
	}
	return items
}

func symbolItemKind(k symbolKind) int {
	switch k {
	case symAgent, symTool, symMemory:
		return kindClass
	case symPrompt, symSkill, symPipeline, symMCPServer:
		return kindProperty
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
