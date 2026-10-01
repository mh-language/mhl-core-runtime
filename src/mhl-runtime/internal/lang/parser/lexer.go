package parser

import "github.com/alecthomas/participle/v2/lexer"

// mhlRegexLexer is the regex-rule table for every token except a
// single-quoted String's own boundary (see string_lexer.go) — wrapped by
// mhlLexer below into the Definition Participle actually uses.
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
// assignment, see ast.AssignStmt) precede it so `x += y` lexes as one
// operator rather than `+` then `=`. A bare `?` is a token only for an
// optional object-shape field (`base?: string`, see ast.ShapeField); it is
// valid nowhere else in the grammar.
//
// The "String" rule below is never actually matched in practice — mhlLexer
// intercepts every single-quoted string before delegating here (see
// string_lexer.go) — but it must stay listed so the token type it names
// exists in Symbols() for the grammar's `@String` references and for
// participle.Unquote("String") to key off of.
var mhlRegexLexer = lexer.MustSimple([]lexer.SimpleRule{
	{Name: "Comment", Pattern: `//[^\n]*`},
	{Name: "Whitespace", Pattern: `[ \t\r\n]+`},
	{Name: "MLString", Pattern: `"""[\s\S]*?"""`},
	{Name: "String", Pattern: `"(\\.|[^"\\])*"`},
	{Name: "Duration", Pattern: `[0-9]+(?:ms|s|m|h|d)\b`},
	{Name: "BadNumber", Pattern: `[0-9]+(?:\.[0-9]+)?[a-zA-Z_][a-zA-Z0-9_]*`},
	{Name: "Number", Pattern: `[0-9]+(?:\.[0-9]+)?`},
	{Name: "Ident", Pattern: `[a-zA-Z_][a-zA-Z0-9_]*`},
	{Name: "Punct", Pattern: `\.\.|->|==|!=|>=|<=|&&|\|\||\?\.|\?\?|\+=|[-+*/%<>=!^(){}\[\]:,.?]`},
})
