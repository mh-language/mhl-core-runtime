// Package parser turns .mh source into a Go AST using a Participle v2-based
// lexer/parser. The public contract is Parse(source) (*ast.Program, error).
package parser

import (
	"regexp"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// rejectBadNumber turns a BadNumber token (a digit run glued directly to
// letters that the lexer could not read as a duration, e.g. `10.0d`, `1e5`,
// `5days`) into a positioned parse error, so the author sees the real
// problem at its source location rather than a downstream "undefined
// variable" once the number and the trailing identifier drift apart.
func rejectBadNumber(t lexer.Token) (lexer.Token, error) {
	return t, participle.Errorf(t.Pos,
		"malformed number %q: a numeric literal cannot be followed directly by letters "+
			"(durations are integers with a unit: 30s, 5m, 2h, 7d)", t.Value)
}

// mhlParser is the compiled Participle parser for the MHL grammar. It is built
// once at package initialization; a build failure indicates a malformed
// grammar definition and is a programmer error, so we panic via MustBuild.
var mhlParser = participle.MustBuild[ast.Program](
	participle.Lexer(mhlLexer),
	participle.Elide("Comment", "Whitespace"),
	// Unquote double-quoted String tokens into their literal value.
	participle.Unquote("String"),
	// Strip the triple-quote delimiters from multi-line strings.
	participle.Map(trimMultiline, "MLString"),
	// A BadNumber token can never be valid — fail with a clear message.
	participle.Map(rejectBadNumber, "BadNumber"),
	// The grammar relies on multi-token lookahead to disambiguate, e.g.
	// named vs positional call arguments and assignment vs expression
	// statements.
	participle.UseLookahead(participle.MaxLookahead),
)

// mhlExprParser is the same grammar as mhlParser, rooted at ast.Expr instead
// of ast.Program. It exists for ${...} string interpolation, where the
// snippet between the delimiters is itself a single expression, not a whole
// .mh file.
var mhlExprParser = participle.MustBuild[ast.Expr](
	participle.Lexer(mhlLexer),
	participle.Elide("Comment", "Whitespace"),
	participle.Unquote("String"),
	participle.Map(trimMultiline, "MLString"),
	participle.UseLookahead(participle.MaxLookahead),
)

// trimMultiline removes the surrounding `"""` delimiters from a multi-line
// string token, leaving the inner content trimmed of surrounding whitespace.
func trimMultiline(t lexer.Token) (lexer.Token, error) {
	v := strings.TrimPrefix(t.Value, `"""`)
	v = strings.TrimSuffix(v, `"""`)
	t.Value = strings.TrimSpace(v)
	return t, nil
}

// leadingDocBlockPattern matches an optional `/** ... **/` block at the very
// start of a source file (only leading whitespace allowed before it) — the
// one block-comment form mhl recognizes, reserved for a file-level doc
// header on an `extensible` declaration (internal/extension/external reads
// its text back off the raw source; see docBlockPattern there). mhl has no
// general block-comment syntax, so Parse strips this one specific shape out
// before lexing — blanking its characters to spaces while keeping every
// newline, so every following line keeps its original line number — rather
// than teach the shared lexer a token that would need eliding (or handling)
// in every other grammar position it could otherwise appear in.
var leadingDocBlockPattern = regexp.MustCompile(`(?s)\A\s*/\*\*.*?\*\*/`)

// stripLeadingDocBlock blanks out a leading `/** ... **/` block, if present,
// so mhlParser never sees it. A source with no such block is returned
// unchanged.
func stripLeadingDocBlock(source string) string {
	loc := leadingDocBlockPattern.FindStringIndex(source)
	if loc == nil {
		return source
	}
	b := []byte(source[loc[0]:loc[1]])
	for i, c := range b {
		if c != '\n' {
			b[i] = ' '
		}
	}
	return source[:loc[0]] + string(b) + source[loc[1]:]
}

// Parse parses .mh source into a complete AST. On a syntactically valid
// source it returns a *ast.Program with a nil error; on malformed input it
// returns a nil program and a descriptive parse error (including position),
// never a partial or best-effort AST.
func Parse(source string) (*ast.Program, error) {
	prog, err := mhlParser.ParseString("", stripLeadingDocBlock(source))
	if err != nil {
		return nil, err
	}
	return prog, nil
}

// ParseExpr parses source as a single MHL expression (the grammar rooted at
// ast.Expr rather than ast.Program), e.g. the snippet inside a "${...}"
// string interpolation span.
func ParseExpr(source string) (*ast.Expr, error) {
	expr, err := mhlExprParser.ParseString("", source)
	if err != nil {
		return nil, err
	}
	return expr, nil
}
