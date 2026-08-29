package parser

import "github.com/alecthomas/participle/v2/lexer"

// mhlLexer is the stateless lexer for .mh source.
//
// Rule ordering is significant: the first rule that matches at a given
// position wins. In particular MLString precedes String (three quotes before
// one), and Duration precedes Number so that `45s` is a single duration token
// rather than a number followed by an identifier. BadNumber sits between the
// two so a digit run glued directly to letters that is NOT a valid duration
// (`10.0d`, `1e5`, `5days`) lexes as one token the grammar has no rule for —
// the parser then rejects it with a clear "malformed number" error (see
// rejectBadNumber in parser.go) instead of splitting it into a number and a
// bare identifier that fails much later as "undefined variable". Within
// Punct, `..` (the
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
	{Name: "BadNumber", Pattern: `[0-9]+(?:\.[0-9]+)?[a-zA-Z_][a-zA-Z0-9_]*`},
	{Name: "Number", Pattern: `[0-9]+(?:\.[0-9]+)?`},
	{Name: "Ident", Pattern: `[a-zA-Z_][a-zA-Z0-9_]*`},
	{Name: "Punct", Pattern: `\.\.|->|==|!=|>=|<=|&&|\|\||\?\.|\?\?|\+=|[-+*/%<>=!^(){}\[\]:,.]`},
})
