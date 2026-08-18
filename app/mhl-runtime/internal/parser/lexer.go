package parser

import "github.com/alecthomas/participle/v2/lexer"

// mhlLexer is the stateless lexer for .mhl source.
//
// Rule ordering is significant: the first rule that matches at a given
// position wins. In particular MLString precedes String (three quotes before
// one), and Duration precedes Number so that `45s` is a single duration token
// rather than a number followed by an identifier.
var mhlLexer = lexer.MustSimple([]lexer.SimpleRule{
	{Name: "Comment", Pattern: `//[^\n]*`},
	{Name: "Whitespace", Pattern: `[ \t\r\n]+`},
	{Name: "MLString", Pattern: `"""[\s\S]*?"""`},
	{Name: "String", Pattern: `"(\\.|[^"\\])*"`},
	{Name: "Duration", Pattern: `[0-9]+(?:ms|s|m|h|d)\b`},
	{Name: "Number", Pattern: `[0-9]+(?:\.[0-9]+)?`},
	{Name: "Ident", Pattern: `[a-zA-Z_][a-zA-Z0-9_]*`},
	{Name: "Punct", Pattern: `->|==|!=|>=|<=|&&|\|\||[-+*/<>=!(){}\[\]:,.]`},
})
