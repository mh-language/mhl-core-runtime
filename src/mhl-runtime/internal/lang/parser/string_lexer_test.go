package parser

import (
	"regexp"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// TestStringLexerHandlesNestedQuotesInInterpolation is the regression test
// for the bug reported against `memory Telemetry { path:
// "...${time.format(time.now(), "yyyy-MM-dd")}..." }`: mhlRegexLexer's
// single-regex "String" rule (`"(\\.|[^"\\])*"`) has no notion of "${...}"
// interpolation, so an unescaped '"' inside one — a perfectly ordinary
// nested string literal argument — used to end the token right there,
// silently truncating it; everything after lexed as unrelated loose
// tokens, and the parser failed many tokens downstream with a confusing
// error nowhere near the real problem. mhlLexer (string_lexer.go) fixes
// this at the lexer boundary; unquoteMHLString fixes the matching
// participle.Unquote replacement so escape handling stays exactly as
// before both inside and outside a "${...}" span.
func TestStringLexerHandlesNestedQuotesInInterpolation(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "unescaped nested string, the reported bug",
			src:  `"projects/telemetry/${time.format(time.now(), "yyyy-MM-dd")}.jsonl"`,
			want: `projects/telemetry/${time.format(time.now(), "yyyy-MM-dd")}.jsonl`,
		},
		{
			name: "old escaped-quote workaround still works unchanged",
			src:  `"retries=${session_mem.get(\"cfg::retries\")}"`,
			want: `retries=${session_mem.get("cfg::retries")}`,
		},
		{
			name: "two interpolations, one with a nested string",
			src:  `"${a}-${f("b")}"`,
			want: `${a}-${f("b")}`,
		},
		{
			name: "nested object literal alongside a nested string",
			src:  `"${ {a: "x", b: 1} }"`,
			want: `${ {a: "x", b: 1} }`,
		},
		{
			name: "a literal '}' inside a nested string doesn't close the span early",
			src:  `"${ f("a}b") } tail"`,
			want: `${ f("a}b") } tail`,
		},
		{
			name: "plain string with no interpolation is unaffected",
			src:  `"plain text, no dollar-brace here"`,
			want: `plain text, no dollar-brace here`,
		},
		{
			name: "ordinary escapes still decode outside interpolation",
			src:  `"line one\nline two \"quoted\""`,
			want: "line one\nline two \"quoted\"",
		},
		{
			name: "empty string",
			src:  `""`,
			want: ``,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := ParseExpr(tc.src)
			if err != nil {
				t.Fatalf("ParseExpr(%s): %v", tc.src, err)
			}
			got, ok := ast.StringValue(expr)
			if !ok {
				t.Fatalf("ParseExpr(%s): not a string literal: %#v", tc.src, expr)
			}
			if got != tc.want {
				t.Errorf("ParseExpr(%s) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestStringLexerRejectsUnterminatedString mirrors the pre-existing
// "lexer: invalid input text" failure for a string with no closing quote —
// scanStringSpan (string_lexer.go) needs to still reject this case, not
// silently run to end-of-file.
func TestStringLexerRejectsUnterminatedString(t *testing.T) {
	_, err := ParseExpr(`"unterminated`)
	if err == nil {
		t.Fatalf("expected an error for an unterminated string, got nil")
	}
}

// TestStringLexerRejectsUnterminatedInterpolation covers a "${" with no
// matching "}" before the string's own closing quote.
func TestStringLexerRejectsUnterminatedInterpolation(t *testing.T) {
	_, err := ParseExpr(`"prefix ${unclosed"`)
	if err == nil {
		t.Fatalf("expected an error for an unterminated interpolation span, got nil")
	}
}

// TestStringLexerPreservesPositionsAfterString checks that a token lexed
// after a string containing a "${...}" span still reports its true source
// position — mhlSourceLexer rebuilds a fresh sub-lexer per token and must
// keep translating its always-relative Position back into absolute
// coordinates correctly (see mhlSourceLexer.Next's doc comment).
func TestStringLexerPreservesPositionsAfterString(t *testing.T) {
	src := "pipeline P {\n    step s {\n        var x = \"${f(\"a\")}\"\n        var y = bogus + + \n    }\n}\n"
	_, err := Parse(src)
	if err == nil {
		t.Fatalf("expected a parse error on the malformed line 4, got nil")
	}
	msg := err.Error()
	if !line4Re.MatchString(msg) {
		t.Errorf("expected the error to point at line 4 (after the interpolated string), got: %s", msg)
	}
}

var line4Re = regexp.MustCompile(`(^|:)4:[0-9]+:`)
