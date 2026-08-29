package parser

import "github.com/alecthomas/participle/v2/lexer"

// mhlLexer is the stateless lexer for .mh source.
//
// Rule ordering is significant: the first rule that matches at a given
// position wins. In particular MLString precedes String (three quotes before
// one), and Duration precedes Number so that `45s` is a single duration token
// rather than a number followed by an identifier. Within Punct, `..` (the
// slice-range separator, see ast.Slice) precedes the single-char class so a
// `..` in source lexes as one token rather than two `.` (member-access)
// tokens; likewise `?.` (optional member access, see ast.Trailer), `??`
// (the null-coalescing operator, see ast.Expr) and `+=` (compound append/add
// assignment, see ast.AssignStmt) precede it so a bare `?` never stands alone
// and `x += y` lexes as one operator rather than `+` then `=`.
var mhlLexer = lexer.MustSimple([]lexer.SimpleRule{
	{Name: "Comment", Pattern: `//[^\n]*`},
	{Name: "Whitespace", Pattern: `[ \t\r\n]+`},
	{Name: "MLString", Pattern: `"""[\s\S]*?"""`},
	{Name: "String", Pattern: `"(\\.|[^"\\])*"`},
	{Name: "Duration", Pattern: `[0-9]+(?:ms|s|m|h|d)\b`},
	{Name: "Number", Pattern: `[0-9]+(?:\.[0-9]+)?`},
	{Name: "Ident", Pattern: `[a-zA-Z_][a-zA-Z0-9_]*`},
	{Name: "Punct", Pattern: `\.\.|->|==|!=|>=|<=|&&|\|\||\?\.|\?\?|\+=|[-+*/%<>=!^(){}\[\]:,.]`},
})
