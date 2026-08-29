// Package parser turns .mh source into a Go AST using a Participle v2-based
// lexer/parser. The public contract is Parse(source) (*ast.Program, error).
package parser

import (
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

// Parse parses .mh source into a complete AST. On a syntactically valid
// source it returns a *ast.Program with a nil error; on malformed input it
// returns a nil program and a descriptive parse error (including position),
// never a partial or best-effort AST.
func Parse(source string) (*ast.Program, error) {
	prog, err := mhlParser.ParseString("", source)
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
