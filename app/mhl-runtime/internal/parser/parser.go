// Package parser turns .mhl source into a Go AST using a Participle v2-based
// lexer/parser. The public contract is Parse(source) (*ast.Program, error).
package parser

import (
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"

	"github.com/yanjustino/mhl-runtime/internal/ast"
)

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
	// The grammar relies on multi-token lookahead to disambiguate, e.g.
	// named vs positional call arguments and assignment vs expression
	// statements.
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

// Parse parses .mhl source into a complete AST. On a syntactically valid
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
