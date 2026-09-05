package lsp

import (
	"regexp"
	"strings"
)

// calleeRe captures the callee token immediately before a "(" — either a
// bare name (`are_equal`, `log`) or a "receiver.method" pair (`git.diff`,
// `session_mem.get`). Whitespace around the dot is tolerated.
var calleeRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)(?:\s*\.\s*([A-Za-z_][A-Za-z0-9_]*))?\s*$`)

// signatureHelpAt answers textDocument/signatureHelp: which built-in call
// the cursor sits inside, and which of its parameters is active. It is a
// best-effort text scan (like the rest of this package), not a real parse —
// it finds the innermost unclosed "(" before the cursor, reads the callee
// in front of it, and counts top-level commas to pick the active parameter.
// Returns nil when the cursor isn't inside a call whose signature is known.
func signatureHelpAt(path, text string, pos position) *signatureHelp {
	prefix := textUpToPosition(text, pos)
	openIdx, commas, ok := enclosingCall(prefix)
	if !ok {
		return nil
	}
	m := calleeRe.FindStringSubmatch(prefix[:openIdx])
	if m == nil {
		return nil
	}

	var (
		sg    sig
		found bool
	)
	if m[2] != "" {
		receiver, method := m[1], m[2]
		for _, s := range documentSymbols(path, text) {
			if s.Name == receiver {
				sg, found = signatureForMethod(path, s, method)
				break
			}
		}
	} else {
		sg, found = signatureForBareCall(m[1])
	}
	if !found {
		return nil
	}

	params := make([]parameterInformation, len(sg.Params))
	for i, p := range sg.Params {
		params[i] = parameterInformation{Label: p}
	}
	active := commas
	if len(sg.Params) > 0 && active >= len(sg.Params) {
		// Clamp past-the-end (a variadic tail, or extra commas) onto the
		// last parameter rather than pointing at nothing.
		active = len(sg.Params) - 1
	}
	var doc *markupContent
	if sg.Doc != "" {
		doc = &markupContent{Kind: "markdown", Value: sg.Doc}
	}
	return &signatureHelp{
		Signatures: []signatureInformation{{
			Label:         sg.Label,
			Documentation: doc,
			Parameters:    params,
		}},
		ActiveSignature: 0,
		ActiveParameter: active,
	}
}

// enclosingCall forward-scans s (the text before the cursor) and returns the
// byte offset of the innermost still-open "(" and how many top-level commas
// follow it up to the cursor. ok is false when the cursor isn't inside any
// unclosed "(". String literals ("..." and """...""") and // line comments
// are skipped so a "(" or "," inside them doesn't count.
func enclosingCall(s string) (openIdx, commas int, ok bool) {
	type frame struct {
		open   int
		commas int
	}
	var stack []frame

	inStr, inTriple, inComment := false, false, false
	var strCh byte

	for i := 0; i < len(s); i++ {
		c := s[i]

		switch {
		case inComment:
			if c == '\n' {
				inComment = false
			}
			continue
		case inTriple:
			if strings.HasPrefix(s[i:], `"""`) {
				inTriple = false
				i += 2
			}
			continue
		case inStr:
			if c == '\\' {
				i++
			} else if c == strCh {
				inStr = false
			}
			continue
		}

		switch {
		case strings.HasPrefix(s[i:], `"""`):
			inTriple = true
			i += 2
		case c == '"':
			inStr, strCh = true, c
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			inComment = true
			i++
		case c == '(':
			stack = append(stack, frame{open: i})
		case c == ')':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case c == ',':
			if len(stack) > 0 {
				stack[len(stack)-1].commas++
			}
		}
	}

	if len(stack) == 0 {
		return 0, 0, false
	}
	top := stack[len(stack)-1]
	return top.open, top.commas, true
}
