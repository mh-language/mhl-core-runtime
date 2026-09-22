package lsp

import "strings"

// hoverAt answers textDocument/hover: the explanation shown for whatever's
// under the cursor. Unlike signature help, it doesn't need to be inside an
// open call — hovering `http`, `os.user`, a declared `tool`/`prompt`/
// `agent`'s own name, anywhere it's written, all resolve. It reuses
// definition.go's identAt/identEndingAt word-extraction (the same "what's
// the cursor on" primitives go-to-definition already relies on) rather
// than a second hand-rolled scanner, and signatures.go's signatureForMethod
// / signatureForBareCall for the "receiver.method"/bare-call cases — so a
// call's hover and its signature help always describe it identically.
// Returns nil when nothing here is resolvable (LSP's "no hover" response).
func hoverAt(path, text string, pos position) *hover {
	line := lineAt(text, pos.Line)
	word, start, ok := identAt(line, pos.Character)
	if !ok {
		return nil
	}

	if start >= 1 && line[start-1] == '.' {
		if receiver, ok := identEndingAt(line, start-1); ok {
			for _, s := range documentSymbols(path, text) {
				if s.Name != receiver {
					continue
				}
				if sg, ok := signatureForMethod(path, text, s, word); ok {
					return sigHover(sg)
				}
				break
			}
		}
	}

	for _, s := range documentSymbols(path, text) {
		if s.Name != word {
			continue
		}
		if s.Kind == symNative {
			return nativeNamespaceHover(s)
		}
		return declHover(path, text, s)
	}

	if sg, ok := signatureForBareCall(path, text, word); ok {
		return sigHover(sg)
	}
	return nil
}

// sigHover renders a sig (signatures.go) as a hover: its Label in a code
// block, then its Doc paragraph when it has one — the same two pieces
// signatureHelpAt already shows, just without needing an open call first.
func sigHover(sg sig) *hover {
	parts := []string{"```mhl\n" + sg.Label + "\n```"}
	if sg.Doc != "" {
		parts = append(parts, sg.Doc)
	}
	return &hover{Contents: markupContent{Kind: "markdown", Value: strings.Join(parts, "\n\n")}}
}

// nativeNamespaceHover renders a bare reserved namespace (`http`, `os`,
// ...) with no member chosen yet — its own name plus the methods it
// offers, so hovering `http` itself (not yet `http.post`) still says
// something.
func nativeNamespaceHover(s symbol) *hover {
	body := "```mhl\n" + s.Name + "\n```\n\nNative namespace. Methods: " + backtickJoin(s.Methods)
	return &hover{Contents: markupContent{Kind: "markdown", Value: body}}
}

// declHover renders a user declaration's own name — an agent/router/
// memory/tool/prompt/pipeline/extension/type/enum — as its kind, its
// nearest leading "//" doc comment (leadingComment) when it has one, and a
// kind-specific detail line: a tool's method names, a prompt's own call
// signature (promptCallSig), or an enum's variants.
func declHover(path, text string, s symbol) *hover {
	parts := []string{"```mhl\n" + s.Kind.label() + " " + s.Name + "\n```"}
	if doc := declLeadingComment(path, text, s); doc != "" {
		parts = append(parts, doc)
	}
	switch s.Kind {
	case symTool:
		if len(s.Methods) > 0 {
			parts = append(parts, "Methods: "+backtickJoin(s.Methods))
		}
	case symPrompt:
		if sg, ok := promptCallSig(path, text, s.Name); ok {
			parts = append(parts, "```mhl\n"+sg.Label+"\n```")
		}
	case symEnum:
		if len(s.Methods) > 0 {
			parts = append(parts, "Variants: "+backtickJoin(s.Methods))
		}
	case symAgent, symMemory:
		if len(s.Methods) > 0 {
			parts = append(parts, "Methods: "+backtickJoin(s.Methods))
		}
	}
	return &hover{Contents: markupContent{Kind: "markdown", Value: strings.Join(parts, "\n\n")}}
}

// declLeadingComment resolves s's own declaration and returns its nearest
// leading "//" doc comment (leadingComment), or "" when it has none or the
// declaration can't be located.
func declLeadingComment(path, text string, s symbol) string {
	_, src, nameOff, _, ok := locateDeclaration(path, text, s.Name, nil)
	if !ok {
		return ""
	}
	return leadingComment(src, offsetToPos(src, nameOff).Line)
}

// leadingComment collects the contiguous "//"-prefixed lines immediately
// above line (0-indexed, offsetToPos's convention) in src — a common doc-
// comment convention mhl's grammar itself gives no special status to (they
// elide during lexing like any other comment, see mhlLexer's doc comment
// in lexer.go), stopping at the first blank or non-comment line above.
// Returned in original top-to-bottom reading order; "" when there is none.
func leadingComment(src string, line int) string {
	lines := strings.Split(src, "\n")
	var collected []string
	for i := line - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "//") {
			break
		}
		collected = append(collected, strings.TrimSpace(strings.TrimPrefix(trimmed, "//")))
	}
	if len(collected) == 0 {
		return ""
	}
	for l, r := 0, len(collected)-1; l < r; l, r = l+1, r-1 {
		collected[l], collected[r] = collected[r], collected[l]
	}
	return strings.Join(collected, " ")
}

// backtickJoin renders each name in code-span form, comma-separated — the
// method/variant list style used throughout declHover/nativeNamespaceHover.
func backtickJoin(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "`" + n + "`"
	}
	return strings.Join(quoted, ", ")
}
