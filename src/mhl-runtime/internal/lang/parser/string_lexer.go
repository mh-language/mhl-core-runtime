package parser

import (
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

// mhlLexer is the Definition Participle actually uses (parser.go). It wraps
// mhlRegexLexer, delegating every token to its regex rules except the start
// of a single-quoted string, which it locates itself with scanStringSpan
// instead of mhlRegexLexer's own "String" rule.
//
// Why: mhlRegexLexer's String pattern (`"(\\.|[^"\\])*"`) is a single regex
// with no notion of "${...}" interpolation, so it ends the token at the
// first unescaped '"' it meets — including one that's part of a perfectly
// ordinary nested string literal inside an interpolation span, e.g. the
// "yyyy-MM-dd" in `"${time.format(x, "yyyy-MM-dd")}"`. That silently
// truncates the token there, and everything after (yyyy, -, MM, ...) lexes
// as unrelated loose tokens — the parser then fails many tokens downstream
// with a confusing error (e.g. `unexpected token "-"`) that points nowhere
// near the actual problem. scanStringSpan tracks "${...}" nesting (and any
// further strings nested inside it, recursively) so an interpolation span
// can contain an ordinary, unescaped string literal — matching what
// interpreter.interpolate() already assumes at runtime when it re-parses
// each "${...}" span as an independent expression via parser.ParseExpr.
var mhlLexer lexer.Definition = &mhlLexerDefinition{base: mhlRegexLexer}

type mhlLexerDefinition struct {
	base *lexer.StatefulDefinition
}

func (d *mhlLexerDefinition) Symbols() map[string]lexer.TokenType {
	return d.base.Symbols()
}

func (d *mhlLexerDefinition) Lex(filename string, r io.Reader) (lexer.Lexer, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return d.LexString(filename, string(data))
}

// LexString is Participle's optional fast path (lexer.StringDefinition);
// mhlRegexLexer itself supports it, and this wrapper does too so
// Parse/ParseExpr (parser.go) never fall back to the slower Reader path.
func (d *mhlLexerDefinition) LexString(filename string, s string) (lexer.Lexer, error) {
	stringType := d.base.Symbols()["String"]
	return &mhlSourceLexer{
		base:       d.base,
		stringType: stringType,
		data:       s,
		pos:        lexer.Position{Filename: filename, Line: 1, Column: 1},
	}, nil
}

// mhlSourceLexer walks data token by token, handing every position except a
// bare string-open quote to a freshly built mhlRegexLexer sub-lexer over
// the remaining data. Rebuilding that sub-lexer per token (rather than
// keeping one running instance across the whole file) is what lets it hand
// off to scanStringSpan mid-stream without the two ever losing sync — a
// live *lexer.StatefulLexer keeps its own private cursor into a copy of
// data, with no exported way to fast-forward it past a span this code
// scanned by hand. LexString is O(1) (it just wraps the string, no regex
// recompilation), so rebuilding it per token costs nothing that matters at
// .mh source-file sizes.
type mhlSourceLexer struct {
	base       *lexer.StatefulDefinition
	stringType lexer.TokenType
	data       string
	pos        lexer.Position
}

func (l *mhlSourceLexer) Next() (lexer.Token, error) {
	if len(l.data) == 0 {
		return lexer.EOFToken(l.pos), nil
	}
	if strings.HasPrefix(l.data, `"`) && !strings.HasPrefix(l.data, `"""`) {
		return l.scanString()
	}
	sub, err := l.base.LexString(l.pos.Filename, l.data)
	if err != nil {
		return lexer.Token{}, err
	}
	tok, err := sub.Next()
	if err != nil {
		return lexer.Token{}, err
	}
	if tok.EOF() {
		return tok, nil
	}
	// sub was just built fresh from l.data, so tok.Value is always an exact
	// prefix of it (every rule pattern is anchored at the current position)
	// and tok.Pos is always {Line:1,Column:1,Offset:0} relative to it — i.e.
	// exactly l.pos, our own running absolute position.
	tok.Pos = l.pos
	l.data = l.data[len(tok.Value):]
	l.pos.Advance(tok.Value)
	return tok, nil
}

// scanString consumes one "..." String token starting at l.data[0] ('"'),
// via scanStringSpan, and reports an unterminated string the same way a
// non-matching mhlRegexLexer rule would (lexer.Error, so it formats and
// surfaces through Participle identically to any other lexer failure).
func (l *mhlSourceLexer) scanString() (lexer.Token, error) {
	end, ok := scanStringSpan(l.data)
	if !ok {
		return lexer.Token{}, &lexer.Error{Msg: "unterminated string literal", Pos: l.pos}
	}
	value := l.data[:end]
	tok := lexer.Token{Type: l.stringType, Value: value, Pos: l.pos}
	l.data = l.data[end:]
	l.pos.Advance(value)
	return tok, nil
}

// scanStringSpan finds the end (exclusive, i.e. one past the closing '"')
// of the double-quoted string starting at s[0]. Plain content and `\x`
// escapes behave exactly like mhlRegexLexer's own "String" pattern
// (`"(\\.|[^"\\])*"`); the one difference is a "${" span, whose "}" is
// found by counting "{"/"}" depth and, for a "\"" met along the way,
// recursing into this same function — so both further brace nesting
// (`${ {a: {b: 1}} }`) and a further nested string (however deep, however
// many interpolations it itself contains) are handled correctly, the same
// way a hand-written recursive-descent scanner would, which a single
// non-recursive regex fundamentally cannot express. ok is false if s ends
// before a terminating quote is found.
func scanStringSpan(s string) (end int, ok bool) {
	i := 1 // past the opening quote
	for i < len(s) {
		switch s[i] {
		case '"':
			return i + 1, true
		case '\\':
			_, size := utf8.DecodeRuneInString(s[i+1:])
			i += 1 + size
		case '$':
			if i+1 < len(s) && s[i+1] == '{' {
				j, ok := scanInterpolationSpan(s, i+2)
				if !ok {
					return 0, false
				}
				i = j
				continue
			}
			i++
		default:
			_, size := utf8.DecodeRuneInString(s[i:])
			i += size
		}
	}
	return 0, false
}

// unquoteMHLString replaces participle.Unquote("String"). Plain Unquote
// runs strconv.UnquoteChar over the whole token text char by char, which
// errors out on any unescaped '"' — exactly the char a "${...}" span's own
// nested string literal now legitimately contains (scanStringSpan already
// let the lexer accept it one stage earlier). unquoteInterpolated decodes
// \-escapes uniformly across the whole token, at any depth, exactly as
// plain Unquote always did (so the pre-existing \" workaround this
// replaces keeps working unchanged) — the one difference is that a bare,
// unescaped '"'/'{'/'}' met while inside a "${...}" span (tracked the same
// way scanStringSpan tracks it) is passed through as a literal character
// instead of being treated as this token's own terminator/escape. That
// keeps each "${...}" span looking like ordinary, re-parseable mhl source
// in the final value — interpreter.interpolate() extracts and re-parses it
// later as its own independent expression (parser.ParseExpr), and needs to
// see real, unescaped quotes there, not participle-escaped ones.
func unquoteMHLString(t lexer.Token) (lexer.Token, error) {
	v, err := unquoteInterpolated(t.Value)
	if err != nil {
		return t, participle.Errorf(t.Pos, "invalid quoted string %q: %s", t.Value, err.Error())
	}
	t.Value = v
	return t, nil
}

func unquoteInterpolated(raw string) (string, error) {
	quote := raw[0]
	s := raw[1 : len(raw)-1]
	var out strings.Builder
	depth := 0 // 0 = plain text; >0 = inside one or more nested "${...}" spans
	for i := 0; i < len(s); {
		switch {
		case s[i] == '\\':
			value, _, tail, err := strconv.UnquoteChar(s[i:], quote)
			if err != nil {
				return "", err
			}
			out.WriteRune(value)
			i = len(s) - len(tail)
		case depth == 0 && s[i] == '$' && i+1 < len(s) && s[i+1] == '{':
			out.WriteString("${")
			depth = 1
			i += 2
		case depth > 0 && s[i] == '{':
			out.WriteByte('{')
			depth++
			i++
		case depth > 0 && s[i] == '}':
			out.WriteByte('}')
			depth--
			i++
		case depth > 0 && s[i] == '"':
			out.WriteByte('"')
			i++
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			out.WriteRune(r)
			i += size
		}
	}
	return out.String(), nil
}

// MatchInterpolationSpan locates the "}" that closes the "${" interpolation
// span starting at s[dollarIdx] (s[dollarIdx] == '$', s[dollarIdx+1] ==
// '{'), returning that closing "}"'s own index. It counts "{"/"}" depth and
// recurses into any double-quoted string met along the way — via the same
// scanStringSpan/scanInterpolationSpan pair the lexer uses to find a
// String token's own boundary — so a "}" that's really just content
// inside a nested string literal (e.g. `${ f("a}b") }`) isn't mistaken for
// the span's own end. Exported for interpreter.interpolate()
// (internal/engine/interpreter/interpolate.go), which re-parses each
// "${...}" span it finds in an already-unquoted string value as its own
// expression (via ParseExpr) and needs the exact same span boundary this
// package's own lexer already computed when it first accepted the token.
func MatchInterpolationSpan(s string, dollarIdx int) (closeBraceIdx int, ok bool) {
	end, ok := scanInterpolationSpan(s, dollarIdx+2)
	if !ok {
		return 0, false
	}
	return end - 1, true
}

// scanInterpolationSpan scans the body of a "${...}" span starting right
// after its "${", returning the index one past its closing "}". Brace
// depth (starting at 1, for the span's own closing brace) accounts for any
// object/block literal inside the interpolated expression; a "\"" met at
// depth >= 1 recurses through scanStringSpan so that span's own content —
// including any "{"/"}"/"\"" it contains — is skipped as a unit rather than
// miscounted as this span's structure.
func scanInterpolationSpan(s string, i int) (end int, ok bool) {
	depth := 1
	for i < len(s) {
		switch s[i] {
		case '{':
			depth++
			i++
		case '}':
			depth--
			i++
			if depth == 0 {
				return i, true
			}
		case '"':
			nestedEnd, ok := scanStringSpan(s[i:])
			if !ok {
				return 0, false
			}
			i += nestedEnd
		case '\\':
			_, size := utf8.DecodeRuneInString(s[i+1:])
			i += 1 + size
		default:
			_, size := utf8.DecodeRuneInString(s[i:])
			i += size
		}
	}
	return 0, false
}
