package lsp

import (
	"regexp"
	"strings"
)

// calleeRe captures the callee token immediately before a "(" — either a
// bare name (`are_equal`, `log`) or a "receiver.method" pair (`git.diff`,
// `session_mem.get`). Whitespace around the dot is tolerated.
var calleeRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)(?:\s*\.\s*([A-Za-z_][A-Za-z0-9_]*))?\s*$`)

// namedArgRe recognizes a call argument's own "name: " prefix — mhl's named-
// argument form (`http.post(url: "x", tls: §)`). Anchored at the start of
// the argument text enclosingCall hands back, so it only ever matches this
// one argument's own leading name, never an unrelated "key:" that happens
// to appear deeper inside its value (an object literal's own field, say).
var namedArgRe = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*:`)

// signatureHelpAt answers textDocument/signatureHelp: which built-in call
// the cursor sits inside, and which of its parameters is active. It is a
// best-effort text scan (like the rest of this package), not a real parse —
// it finds the innermost unclosed "(" before the cursor and, for the
// argument currently being typed, prefers its own "name: " prefix over
// position: mhl calls bind named arguments in any order (language-design.md
// — "Calls accept positional or named arguments"), so `http.post(url: "x",
// tls: §)` must highlight `tls`, the 11th declared parameter, not the 3rd
// one, which counting top-level commas alone would get wrong the moment a
// caller skips ahead past optional parameters instead of filling every one
// in declared order. A bare/positional argument with no "name: " prefix
// still falls back to the plain comma count, unchanged from before this.
// Returns nil when the cursor isn't inside a call whose signature is known.
func signatureHelpAt(path, text string, pos position) *signatureHelp {
	prefix := textUpToPosition(text, pos)
	openIdx, commas, argStart, ok := enclosingCall(prefix)
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
				sg, found = signatureForMethod(path, text, s, method)
				break
			}
		}
	} else {
		sg, found = signatureForBareCall(path, text, m[1])
	}
	if !found {
		return nil
	}

	params := make([]parameterInformation, len(sg.Params))
	for i, p := range sg.Params {
		params[i] = parameterInformation{Label: p}
		if doc, ok := sg.ParamDocs[p]; ok {
			params[i].Documentation = &markupContent{Kind: "markdown", Value: doc}
		}
	}
	active := commas
	if len(sg.Params) > 0 && active >= len(sg.Params) {
		// Clamp past-the-end (a variadic tail, or extra commas) onto the
		// last parameter rather than pointing at nothing.
		active = len(sg.Params) - 1
	}
	if nm := namedArgRe.FindStringSubmatch(prefix[argStart:]); nm != nil {
		for i, p := range sg.Params {
			if p == nm[1] {
				active = i
				break
			}
		}
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
// byte offset of the innermost still-open "(", how many top-level commas
// follow it up to the cursor, and argStart — the offset where the argument
// currently being typed begins (right after that "("'s own most recent
// top-level comma, or right after the "(" itself for the first argument).
// s[argStart:] is what namedArgRe checks for a "name: " prefix. ok is false
// when the cursor isn't inside any unclosed "(". String literals ("..." and
// """...""") and // line comments are skipped so a "(", ",", or ":" inside
// them doesn't count.
func enclosingCall(s string) (openIdx, commas, argStart int, ok bool) {
	type frame struct {
		open     int
		commas   int
		argStart int
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
			stack = append(stack, frame{open: i, argStart: i + 1})
		case c == ')':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case c == ',':
			if len(stack) > 0 {
				stack[len(stack)-1].commas++
				stack[len(stack)-1].argStart = i + 1
			}
		}
	}

	if len(stack) == 0 {
		return 0, 0, 0, false
	}
	top := stack[len(stack)-1]
	return top.open, top.commas, top.argStart, true
}
